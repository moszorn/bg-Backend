package game

import (
	"container/ring"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/moszorn/pb"
	utilog "github.com/moszorn/utils/log"
	"github.com/moszorn/utils/rchanr"
	"github.com/moszorn/utils/skf"
)

// RoomManager 管理進入房間的所有使用者,包含廣播所有房間使用者,發送訊息給指定玩家
// 未來可能會分方位(RoomZorn),禁言,聊天可能都透過RoomManager
type (

	// 對遊戲桌table 操作或請求
	tableRequest struct {
		topic      tableTopic //請求項目(IsPlayerOnSeat, IsGameStart,  SeatShift, PlayerAction, _GetTablePlayers, _GetZoneUsers, _FindPlayer)
		user       *RoomUser  // 項目 IsPlayerOnSeat, EnterGame, LeaveGame 需要此參數
		player     *RoomUser  //
		shiftSeat  uint8      // SeatShift  需要此參數
		actionSeat uint8      // PlayerAction  需要此參數
	}

	// 操作或請求執行結果
	chanResult struct {
		err error

		e *RoomUser //east 玩家
		w *RoomUser //west 玩家
		s *RoomUser //south 玩家
		n *RoomUser //north 玩家

		// 代表所有Zone的觀眾連線資料結構,不含Player連線
		audiences Audiences
		// 代表以空位為始點的環形元素陣列
		seatOrders [4]*RoomUser

		//代表一個玩家的連線
		player *skf.NSConn

		seat        uint8
		isGameStart bool

		//表示遊戲已經幾人動作了(回合數)
		aa uint8

		//玩家是否已入座
		isOnSeat bool
	}

	// 廣播請求
	broadcastRequest struct {
		msg    *skf.Message
		sender *skf.NSConn // sender != nil 表聊天訊息(除了sender所有人都會發送), sender == nil 表示所有人都會發送(例如:管理,公告訊息,一般訊息)
		to     *skf.NSConn // (預留)私人訊息發送 , to != nil 表示私訊

		//chat 與 admin同時 false 表示一般訊息發送
		chat  bool // 聊天訊息 chat = true 訊息分(私人,公開)所以需要再判斷 sender, to
		admin bool // 管理,公告訊息, 除了admin,chat 同為false是允許的外, admin 與 chat 是互斥的也就不會有 chat = true, admin = true
	}

	// tablePlayer就是Ring Item,代表四方座位的玩家,因此一經初始化後玩家入桌與離桌只會變更player屬性,不會銷毀這個ref
	tablePlayer struct {
		player *RoomUser
		zone   uint8 //代表player座位(CbSeat)東南西北,每個SeatItem初始化時必須指派一個不能重覆的位置
		value  uint8 //當前打出什麼牌(Card)
	}

	ZoneUsers map[*skf.NSConn]*RoomUser

	RoomZoneUsers map[uint8]ZoneUsers

	RoomManager struct {
		// ----------- close Room by cancel func
		shutdown context.Context

		//-------RR chan ------------
		door         rchanr.ChanReqWithArguments[*RoomUser, chanResult]     //user 出入房間
		table        rchanr.ChanReqWithArguments[*tableRequest, chanResult] //遊戲桌詢問
		broadcastMsg rchanr.ChanReqWithArguments[*broadcastRequest, AppErr] //房間廣播

		//Table Player Ring -----
		*ring.Ring
		//回合計數 aa為4表示一回合, aa<4表示回合中
		aa      uint8 // aa(action accumulate) 表示是否完成一回合.(收到叫牌數,或出牌數,滿4個表示一個回合), 預設值:0
		players uint8 // //計數已經入座的座位數,當players == 4 表示遊戲開始

		//------ Room Users -------
		Users    RoomZoneUsers
		ticketSN int //目前房間人數流水號,從1開始

		//------
		g *Game
	}
)

// NewRoomManager RoomManager建構子
func newRoomManager(shutdown context.Context) *RoomManager {
	//Player
	roomZoneUsers := make(map[uint8]ZoneUsers)

	//make Player
	for idx := range playerSeats {
		roomZoneUsers[playerSeats[idx]] = make(map[*skf.NSConn]*RoomUser)
	}
	// Table環形結構設定(東南西北)
	r := ring.New(PlayersLimit)
	for i := 0; i < PlayersLimit; i++ {
		r.Value = &tablePlayer{
			zone: playerSeats[i],
			player: &RoomUser{
				NsConn:      nil,
				PlayingUser: pb.PlayingUser{Zone: uint32(playerSeats[i])},
				Zone8:       playerSeats[i],
			}, /*player一經初始化後永不銷毀*/
		}
		r = r.Next()
	}
	var mr *RoomManager = new(RoomManager)
	mr.shutdown = shutdown
	mr.Users = roomZoneUsers
	mr.door = make(chan rchanr.ChanRepWithArguments[*RoomUser, chanResult])
	mr.table = make(chan rchanr.ChanRepWithArguments[*tableRequest, chanResult])
	mr.broadcastMsg = make(chan rchanr.ChanRepWithArguments[*broadcastRequest, AppErr])
	mr.Ring = r
	return mr
}

// Start RoomManager開始幹活,由Game執行
func (mr *RoomManager) Start() {
	start := true
	for start {
		select {
		case <-mr.shutdown.Done():
			//TODO 關閉所有Room 資源

			start = false
			return
		//坑: 這裡只能針對 gateway channel
		case tracking := <-mr.door:
			user := tracking.Question
			switch user.Tracking {
			case EnterRoom:
				result := chanResult{}
				if _, exist := mr.getRoomUser(user.NsConn); exist {
					result.err = ErrUserInRoom
				}
				if mr.ticketSN > RoomUsersLimit {
					result.err = ErrRoomFull
				} else {

					user.Ticket()
					//房間進入者流水編號累增
					mr.ticketSN++

					// 玩家加入遊戲房間
					mr.Users[user.Zone8][user.NsConn] = user
					result.err = nil //成功入房
					result.isGameStart = mr.players >= 4
				}
				tracking.Response <- result
			case LeaveRoom:

				// 移除離開玩家. EnterRoom時的value也一並移除參考
				delete(mr.Users[user.Zone8], user.NsConn)

				//房間進入者流水編號遞減
				mr.ticketSN--

				//為何這裡需要將設定user為nil,是因要釋放在UserLeave時的記憶體參考
				user = nil

				tracking.Response <- chanResult{
					err:         nil,
					isGameStart: mr.players >= 4,
				}
			case EnterGame:
				//外界在呼叫 EnterGame前,要先判斷遊戲是否開始,玩家是否已經入桌
				seat, gameStart := mr.playerJoin(user, pb.SeatStatus_SitDown)
				result := chanResult{}
				result.seat = seat /* seat若為valueNotSet 表桌已滿,並且gameStart會是 true*/
				result.isGameStart = gameStart
				result.isOnSeat = seat != valueNotSet
				result.err = nil
				tracking.Response <- result
			case LeaveGame:
				seat, gameStart := mr.playerJoin(user, pb.SeatStatus_StandUp)
				result := chanResult{}
				result.seat = seat
				result.isOnSeat = seat != valueNotSet
				result.isGameStart = gameStart
				result.err = nil
				tracking.Response <- result
			}
		case crwa := <-mr.table:
			//crwa (ChanResponseWithArgument)
			req := crwa.Question
			switch req.topic {
			case IsPlayerOnSeat:
				found := false
				limit := PlayersLimit
				seat := mr.Value.(*tablePlayer)
				for limit > 0 && !found {
					limit--
					if seat.player != nil && seat.player == req.user {
						found = true
					}
					mr.Ring = mr.Next()
					seat = mr.Value.(*tablePlayer)
				}

				result := chanResult{}
				if found { //表存已在遊戲中
					result.isOnSeat = true
				} else {
					result.isOnSeat = false
				}
				result.err = nil
				crwa.Response <- result

			case IsGameStart:
				result := chanResult{}
				result.isGameStart = mr.players >= 4
				result.err = nil
				crwa.Response <- result

			case SeatShift:
				result := chanResult{}
				result.seat = mr.seatShift(req.shiftSeat)
				result.isGameStart = mr.players >= 4
				result.aa = mr.aa
				result.err = nil
				crwa.Response <- result

			case PlayerAction:

				//打出一張牌, 這裡應該還要再回傳
				//1. 出牌儲牌否
				//3. 是否一回合,是否最後一張
				//2. 四家牌面 seatPlays() ,
				//某些條件成立時,執行 resetPlay 動作, seatShifging

				result := chanResult{}

				if mr.aa >= 4 {
					result.seat = req.player.Zone8 //user.Player
					result.aa = mr.aa
					result.err = nil
					crwa.Response <- result
					// 注意: break 會直接下一個循環,因此break後面會被忽略
					break
				}

				if !mr.savePlayerCardValue(req.player) {
					result.err = errors.New("座位打出的牌有誤")
					result.seat = req.player.Zone8
					result.aa = mr.aa
					result.isGameStart = mr.players >= 4
					result.err = nil
					crwa.Response <- result
					// 注意: break 會直接下一個循環,因此break後面會被忽略
					break
				}

				mr.aa++
				result.seat = req.player.Zone8
				result.aa = mr.aa
				result.err = nil
				crwa.Response <- result
			case _FindPlayer:

				result := chanResult{}
				result.isGameStart = mr.players >= 4
				result.aa = mr.aa

				var ringItem *tablePlayer
				ringItem, result.isOnSeat = mr.findPlayer(req.player.Zone8)

				if result.isOnSeat {
					//找到指定玩家連線
					result.player = ringItem.player.NsConn
				}
				result.err = nil
				crwa.Response <- result

			case _GetTablePlayers:
				result := chanResult{}
				result.e, result.s, result.w, result.n = mr.tablePlayers()
				result.err = nil
				crwa.Response <- result
			case _GetZoneUsers:
				//撈取 Player Block連線
				result := chanResult{}
				result.aa = mr.aa
				result.isGameStart = mr.players >= 4
				result.audiences, result.e, result.s, result.w, result.n = mr.zoneUsers()
				result.err = nil
				crwa.Response <- result
			case _GetTableInfo:
				result := chanResult{}
				result.seatOrders = mr.lastLeaveOrder()
				result.audiences, _, _, _, _ = mr.zoneUsers()
				result.err = nil
				result.aa = mr.aa
				result.isGameStart = mr.players >= 4
				crwa.Response <- result
			} /*eofSwitch*/

		case send := <-mr.broadcastMsg:
			msg := send.Question
			send.Response <- mr.broadcast(msg)
		default:
			// 移除突然斷線的user
			//g.rmClosedUsers()

		}
	}
}

// getRoomUser 是否連線已經存在房間
func (mr *RoomManager) getRoomUser(nsConn *skf.NSConn) (found *RoomUser, isExist bool) {
	for i := range playerSeats {
		if found, isExist = mr.getZoneRoomUser(nsConn, playerSeats[i]); isExist {
			return
		}
	}
	return
}

// getZoneRoomUser 是否連線已經存在房間某個Zone
func (mr *RoomManager) getZoneRoomUser(nsconn *skf.NSConn, zone uint8) (found *RoomUser, isExist bool) {
	found, isExist = mr.Users[zone][nsconn]
	return
}

// RoomInfo 房間人數,座位狀態,(TODO) 桌面遊戲狀態; 使用者進入房間時需要此資訊
func (mr *RoomManager) RoomInfo(user *RoomUser) {
	//桌中座位順序  seatPlays
	//玩家名稱人數
	//當前桌面狀況

	tqs := &tableRequest{
		topic: _GetTableInfo,
	}

	rep := mr.table.Probe(tqs)
	if rep.err != nil {
		slog.Error("取得RoomInfo錯誤", utilog.Err(rep.err))
	}

	var pp pb.TableInfo = pb.TableInfo{}

	//有順序的四個座位資訊(從第一個空位開始)
	pp.Players = make([]*pb.PlayingUser, 0, PlayersLimit)
	for i := range rep.seatOrders {
		pp.Players = append(pp.Players, &rep.seatOrders[i].PlayingUser)
	}

	//觀眾資訊
	pp.Audiences = make([]*pb.PlayingUser, 0, len(rep.audiences))
	for i := range rep.audiences {
		pp.Audiences = append(pp.Audiences, &rep.audiences[i].PlayingUser)
	}

	payload := payloadData{
		ProtoData:   &pp,
		PayloadType: ProtobufType,
	}

	if err := mr.send(user.NsConn, payload, ClnRoomEvents.UserPrivateTableInfo); err != nil {
		slog.Error("RoomInfo proto錯誤", utilog.Err(err))
	}
}

// UserJoin 使用者進入房間, 必須參數RoomUser {*skf.NSConn, userName, userZone}
func (mr *RoomManager) UserJoin(user *RoomUser) {

	//TBC 好像 Tracking只用來當成 switch的判斷,不需要使用 preTracking 這個機制
	// TODO 移除 preTracking
	preTracking := user.Tracking
	user.TicketTime = pb.LocalTimestamp(time.Now())
	user.Tracking = EnterRoom

	var response chanResult

	//Probe內部用user name查詢是否user已經入房間
	response = mr.door.Probe(user)

	// 房間已滿(超出RoomUsersLimit), 或使用者已存在房間
	if response.err != nil {
		//TODO 移除 Tracking還原
		user.Tracking = preTracking
		slog.Debug("使用者進入房間(UserJoin)", utilog.Err(response.err))
		if user.NsConn != nil && !user.NsConn.Conn.IsClosed() {
			user.NsConn.Emit(ClnRoomEvents.ErrorSpace, []byte(response.err.Error()))
		}
		user = nil
		return
	}

	mr.g.CounterAdd(user.NsConn, mr.g.name)

	//告知client 切換到房間
	//ns.Emit(project.ClnRoomEvents.Private, []byte("你已經入房"))
	//ns.Emit(skf.OnRoomJoined, nil)

	mr.RoomInfo(user)

	//TODO 廣播有人進入房間
}

// UserLeave 使用者離開房間
func (mr *RoomManager) UserLeave(user *RoomUser) {

	//TBC 好像 Tracking只用來當成 switch的判斷,不需要使用 preTracking 這個機制
	// TODO 移除 preTracking
	preTracking := user.Tracking
	user.Tracking = LeaveRoom

	response := mr.door.Probe(user)

	if response.err != nil {
		//TODO 移除 Tracking還原
		user.Tracking = preTracking
		slog.Debug("使用者離開房間(UserLeave)", utilog.Err(response.err))
		if user.NsConn != nil && !user.NsConn.Conn.IsClosed() {
			user.NsConn.Emit(ClnRoomEvents.ErrorSpace, []byte(response.err.Error()))
		}
		user = nil
		return
	}

	//移到 NamespaceDisconnected
	mr.g.CounterSub(user.NsConn, mr.g.name)

	//告知client切換回大廳
	user.NsConn.Emit(skf.OnRoomLeft, nil)
	//ns.Emit(skf.OnRoomLeft, []byte(fmt.Sprintf("已順利離開%s遊戲房", mr.roomNameId)))

	//TODO 廣播有人離開房間

}

// PlayerJoin 加入, 底層透過呼叫 playerJoin, 最後判斷使否開局,與送出發牌
func (mr *RoomManager) PlayerJoin(user *RoomUser) {

	user.Tracking = EnterGame

	var response chanResult

	//Probe內部用user name查詢是否user已經入房間
	response = mr.door.Probe(user)

	// 房間已滿(超出RoomUsersLimit), 或使用者已存在房間
	if response.err != nil {
		slog.Debug("使用者進入房間(UserJoin)", utilog.Err(response.err))
		if user.NsConn != nil && !user.NsConn.Conn.IsClosed() {
			user.NsConn.Emit(ClnRoomEvents.ErrorSpace, []byte(response.err.Error()))
		}
		return
	}

	// 房間已滿,已經晚一步
	if response.isGameStart && !response.isOnSeat {
		user.NsConn.Emit(skf.OnRoomJoined, nil)
		return
	}

	//第0步: 儲存seat到Connection Store,表示這個Connection是一個玩家
	// 注意
	user.NsConn.Conn.Set(KeySeat, CbSeat(response.seat))

	// 第一步: 上桌
	// 告訴玩家你已經上桌,前端必須處理
	user.NsConn.Emit(ClnRoomEvents.TablePrivateOnSeat, []byte{response.seat >> 1})

	// 廣播已經有人上桌,前端必須處理
	load := payloadData{
		ProtoData:   nil, // ______________________________________, // TODO: 送 protobuf payload
		PayloadType: ProtobufType,
	}

	mr.SendPayloadsToZone([]payloadData{load}, ClnRoomEvents.TableOnSeat)

	// 順利坐到位置剛好滿四人局開始
	if response.isOnSeat && response.isGameStart {

		//第二步:  發牌,  前端必須處理
		mr.SendDeal(&mr.g.deckInPlay)

		//第三步 亂數取得開叫者,及禁叫品項
		bidder, forbidden, _ := mr.g.start()

		//第三步: 提示開叫
		//第一個表示上一個叫者座位(因為是首叫,所以上一個叫者為valueNotSet)
		//第二個表示上一個叫者叫品CbBid(上一次叫品,因為是第一次叫所以叫品是valueNotSet)
		//第三個表示下一個叫牌者
		var payload []uint8
		payload = append(payload, valueNotSet, valueNotSet, bidder>>1)
		//最後一個是禁叫品項
		payload = append(payload, forbidden...)

		//延遲,是因為最後進來的玩家前端render速度太慢,會導致接收到NotyBid時來不及,所以延遲幾秒
		time.Sleep(time.Millisecond * 700)

		//個人開叫提示, 前端必須處理
		user.NsConn.EmitBinary(ClnRoomEvents.GamePrivateNotyBid, payload)

		//廣播提示開叫開始, 前端必須處理
		mr.BroadcastByte(ClnRoomEvents.GameNotyBid, mr.g.name, bidder>>1)
	}

}

// PlayerLeave 加入, 底層透過呼叫 playerJoin, 進行離桌程序
func (mr *RoomManager) PlayerLeave(user *RoomUser) {

	user.Tracking = LeaveGame

	var response chanResult

	//Probe內部用user name查詢是否user已經入房間
	response = mr.door.Probe(user)

	// 房間已滿(超出RoomUsersLimit), 或使用者已存在房間
	if response.err != nil {
		slog.Debug("使用者進入房間(UserJoin)", utilog.Err(response.err))
		if user.NsConn != nil && !user.NsConn.Conn.IsClosed() {
			user.NsConn.Emit(ClnRoomEvents.ErrorSpace, []byte(response.err.Error()))
		}
		return
	}

	// 表示發生問題,
	if response.isOnSeat {
		//ns.Emit(skf.OnRoomJoined, nil)
		//紀錄 Log
		// 告訴玩家你已經上桌,前端必須處理
		user.NsConn.Emit(ClnRoomEvents.Private, nil)
		return
	}

	//成功離開座位, 前端必須處理
	user.NsConn.Emit(ClnRoomEvents.TablePrivateOnLeave, nil)

	//廣播已經有人上桌,前端必須處理
	load := payloadData{
		ProtoData:   nil, //   _________________________, // TODO: 送 protobuf payload
		PayloadType: ProtobufType,
	}

	mr.SendPayloadsToZone([]payloadData{load}, ClnRoomEvents.TableOnLeave)
}

// PlayerJoin表示使用者要入桌入座,或離開座位
func (mr *RoomManager) playerJoin(user *RoomUser, flag pb.SeatStatus) (zoneSeat uint8, isGameStart bool) {
	/*
		 user 入座的使用者, flag 旗標表示入座還是離座
		 flag
			 入座時(SeatStatus_SitDown)
			   zoneSeat 表示坐定的座位, isGameStart=false(遊戲尚未開始),isGameStart=ture(遊戲剛好入座開始)
			   zoneSeat 若為valueNotSeat,表示 mr.players  >=4 表示遊戲人數已滿有人搶先入座
			 離座時(SeatStatus_StandUp) zoneSeat 表示成功離座的座位
	*/

	//避免memory leak
	atTime := pb.LocalTimestamp(time.Now())

	zoneSeat = valueNotSet
	var seatAt *tablePlayer
	for i := 0; i < PlayersLimit; i++ {
		seatAt = mr.Value.(*tablePlayer)
		mr.Ring = mr.Next()

		switch flag {
		case pb.SeatStatus_SitDown:
			// Ring player.NsConn == nil 表示有空位
			if seatAt.player.NsConn == nil {

				//注意用copy的
				seatAt.player.NsConn = user.NsConn
				seatAt.player.TicketTime = atTime /*入房間時間*/
				seatAt.player.Name = user.Name

				zoneSeat = seatAt.zone // 入座
				user.Tracking = EnterGame
				mr.players++
				//回傳的zoneSeat不可能是 0x0
				return zoneSeat, mr.players >= 4
			}
		case pb.SeatStatus_StandUp:
			if seatAt.player.NsConn != nil && seatAt.player.NsConn == user.NsConn {
				seatAt.player.NsConn = nil // 離座
				seatAt.player.Play = uint32(valueNotSet)
				seatAt.player.Bid = uint32(valueNotSet)
				seatAt.player.Name = ""

				zoneSeat = seatAt.zone // 離那個座
				seatAt.player.Zone = uint32(valueNotSet)

				user.Tracking = EnterRoom
				mr.players--
				//回傳的zoneSeat不可能是 0x0
				return zoneSeat, mr.players >= 4
			}
		}
	}
	// 可能位置已滿,zoneSeat會是 valueNotSet,所以呼叫者可以判斷
	return zoneSeat, mr.players >= 4
}

// 儲存玩家(座位)的出牌到Ring中,因為回合比牌會從Ring中取得
func (mr *RoomManager) savePlayerCardValue(player *RoomUser) (isSaved bool) {
	if found, exist := mr.findPlayer(uint8(player.Zone)); exist {
		if found.player.NsConn == player.NsConn {
			found.value = uint8(player.Play)
			return true
		}
	}
	return
}

// findPlayer 回傳指定座位上的玩家以 Ring item (*tablePlayer) 回傳
func (mr *RoomManager) findPlayer(seat uint8) (player *tablePlayer, exist bool) {
	// seat 指定座位, exist 找到否, player 回傳的Ring item若exist為true

	tp := mr.Value.(*tablePlayer)
	if tp.zone == seat /**/ {
		return tp, true
	}

	mr.Ring = mr.Next()
	tp = mr.Value.(*tablePlayer)

	found := false
	limit := PlayersLimit - 1
	for limit > 0 && !found {
		limit--
		if tp.zone == seat {
			found = true
			return tp, found
		}
		mr.Ring = mr.Next()
		tp = mr.Value.(*tablePlayer)
	}
	return nil, found
}

// zoneUsersByMap 四個Zone中的Users有效連線, 每個Zone都牌排除 player
func (mr *RoomManager) zoneUsersByMap() (users map[uint8][]*skf.NSConn, ePlayer, sPlayer, wPlayer, nPlayer *RoomUser) {
	// 有可能 Player 中零個 User 連線  len(conn[seat]) => 0
	// players 表示四位玩家,正在遊戲桌上的四位玩家,有可能 player.NsConn 為 nil (網家斷線)

	//玩家連線
	ePlayer, sPlayer, wPlayer, nPlayer = mr.tablePlayers()

	//觀眾連線
	users = make(map[uint8][]*skf.NSConn)

	var (
		zone   uint8
		player *skf.NSConn
	)

	for i := range playerSeats {
		zone = playerSeats[i]
		users[zone] = make([]*skf.NSConn, 0, len(mr.Users[zone])-1) //-1 扣掉Player佔額
		switch zone {
		case east: //east
			player = ePlayer.NsConn
		case south: //south
			player = sPlayer.NsConn
		case west: //west
			player = wPlayer.NsConn
		case north: // north
			player = nPlayer.NsConn
		}
		for conn := range mr.Users[zone] {
			if !conn.Conn.IsClosed() && conn != player {
				users[zone] = append(users[zone], conn)
			}
		}
	}
	return
}

// 區域連線
// zoneUsers 回傳觀眾,與四位玩家(ns可能 nil)
func (mr *RoomManager) zoneUsers() (users []*RoomUser, ePlayer, sPlayer, wPlayer, nPlayer *RoomUser) {
	// users 表示所有觀眾使用者連線, 東南西北玩家(player)分別是 ePlayer, sPlayer, wPlayer, nPlayer

	//玩家連線
	ePlayer, sPlayer, wPlayer, nPlayer = mr.tablePlayers()

	//觀眾連線
	users = make([]*RoomUser, 0, len(mr.Users)-4) //-4 扣除四位玩家

	var (
		player *skf.NSConn
		zone   uint8
	)
	for i := range playerSeats {
		zone = playerSeats[i]
		switch zone {
		case east:
			player = ePlayer.NsConn
		case south:
			player = sPlayer.NsConn
		case west:
			player = wPlayer.NsConn
		case north:
			player = nPlayer.NsConn
		}
		for conn, roomUser := range mr.Users[zone] {
			if !conn.Conn.IsClosed() && conn != player {
				users = append(users, roomUser)
			}
		}
	}
	return
}

// 撈出正在遊戲桌上的四位玩家,有可能 player.NsConn 為 nil (網家斷線)
func (mr *RoomManager) tablePlayers() (e, s, w, n *RoomUser) {
	mr.Do(func(i any) {
		v := i.(*tablePlayer)
		switch v.zone {
		case east:
			e = v.player
		case south:
			s = v.player
		case west:
			w = v.player
		case north:
			n = v.player
		}
	})
	return
}

// 回傳以第一個空位為始點的環形陣列,order 第一個元素就是空位的seat,用於使用者進入房間的位置方位
func (mr *RoomManager) lastLeaveOrder() (order [4]*RoomUser) {
	var limit = PlayersLimit
	order = [PlayersLimit]*RoomUser{}

	var table *tablePlayer = mr.Value.(*tablePlayer)

	//先找出第一個空位發生處,並移動環型結構,直到找到break
	for limit > 0 {
		limit--
		//空位條件 Name=="" , connection == nil
		if table.player.Name == "" && table.player.NsConn == nil {
			break
		}
		mr.Ring = mr.Next()
		table = mr.Value.(*tablePlayer)
	}

	//此時環形會是以第一個找到的空位為始點
	i := 0
	mr.Do(func(seat any) {
		order[i] = (seat.(*tablePlayer)).player
		i++
	})
	return //最後離座順序
}

// PlayersCardValue 撈取四位玩家打出的牌, 回傳的順序固定為 e(east), s(south), w(west), n(north)
func (mr *RoomManager) PlayersCardValue() (e, s, w, n uint8) {
	// TODO 是否需要 Lock 存取
	mr.Do(func(i any) {
		v := i.(*tablePlayer)
		switch v.zone {
		case east:
			e = v.value
		case south:
			s = v.value
		case west:
			w = v.value
		case north:
			n = v.value
		}
	})
	return
}

// 清空還原玩家手上持牌
func (mr *RoomManager) resetPlayersCardValue() {
	mr.aa = 0x0
	mr.Do(func(i any) {
		i.(*tablePlayer).value = valueNotSet
	})
}

// 移動到指定座位,並回傳下一座位
func (mr *RoomManager) seatShift(seat uint8) uint8 {
	player := mr.Value.(*tablePlayer)
	if player.zone == seat {
		//回傳下一座位
		return mr.Next().Value.(*tablePlayer).zone
	}
	for {
		mr.Ring = mr.Next()
		if mr.Value.(*tablePlayer).zone == seat {
			//回傳下一座位
			return mr.Next().Value.(*tablePlayer).zone
		}
	}
}

// SeatShift 移動座位,移動後並回傳下一座位
func (mr *RoomManager) SeatShift(seat uint8) (next uint8) {
	tqs := &tableRequest{
		shiftSeat: seat,
		topic:     SeatShift,
	}

	response := mr.table.Probe(tqs)

	if response.err != nil {
		slog.Debug("移動位置SeatShift", utilog.Err(response.err))
		return valueNotSet
	}
	slog.Debug("移動位置SeatShift", slog.Bool("遊戲開始", response.isGameStart), slog.Int("回合動作", int(response.aa)))
	return response.seat
}

// 從Ring中取得遊戲中四家連線
func (mr *RoomManager) acquirePlayerConnections() (e, s, w, n *skf.NSConn) {
	//step1 以 seat 從Ring找出NsConn
	request := &tableRequest{
		topic: _GetTablePlayers,
	}

	response := mr.table.Probe(request)

	if response.err != nil {
		slog.Error("連取得線出錯(acquirePlayerConnections)", utilog.Err(response.err))
		return
	}
	return response.e.NsConn, response.s.NsConn, response.w.NsConn, response.n.NsConn
}

//SendXXXX 指資訊個別的送出給玩家,觀眾通常用於遊戲資訊
/* ============================================================================================
 BroadcastXXXX 用於廣播,無論玩家,觀眾都會同時送出同樣的訊息,通常用於公告,聊天資訊,遊戲共同訊息(叫牌)
======================== ====================================================================== */

// SendDealToPlayer 向入座遊戲中的玩家發牌,與SendDealToZone不同, SendDealToPlayer向指定玩家發牌
func (mr *RoomManager) sendDealToPlayer(deckInPlay *map[uint8]*[NumOfCardsOnePlayer]uint8, connections ...*skf.NSConn) {
	// playersHand 以Seat為Key,Value代表該Seat的待發牌
	// deckInPlay 由 Game傳入
	// 注意: connections 與 deckInPlay順序必須一致 (ease, south, west, north)
	var player *skf.NSConn
	for idx := range connections {
		player = connections[idx]
		if player != nil && !player.Conn.IsClosed() {
			player.EmitBinary(
				ClnRoomEvents.GamePrivateDeal,
				(*deckInPlay)[playerSeats[idx]][:])
		} else {
			//TODO 其中有一個玩家斷線,就停止遊戲,並通知所有玩家, Player
			slog.Error("連線(SendDeal)中斷", utilog.Err(fmt.Errorf("%s發牌連線中斷", CbSeat(playerSeats[idx]))))
		}
	}
}

// SendDealToZone 向 Zone發牌, 但是必須濾除掉在該Zone的 Player, 因為 Player是透過 SendDealToPlayer發牌
func (mr *RoomManager) sendDealToZone(deckInPlay *map[uint8]*[NumOfCardsOnePlayer]uint8, users []*skf.NSConn) {
	// 4個座位player手持牌
	eHand, sHand, wHand, nHand := (*deckInPlay)[playerSeats[0]][:], (*deckInPlay)[playerSeats[1]][:], (*deckInPlay)[playerSeats[2]][:], (*deckInPlay)[playerSeats[3]][:]
	for i := range users {
		users[i].EmitBinary(ClnRoomEvents.GameDeal, eHand)
		users[i].EmitBinary(ClnRoomEvents.GameDeal, sHand)
		users[i].EmitBinary(ClnRoomEvents.GameDeal, wHand)
		users[i].EmitBinary(ClnRoomEvents.GameDeal, nHand)
	}
}

// SendDeal 向玩家, 觀眾(Player)發牌, 送出 bytes
func (mr *RoomManager) SendDeal(deckInPlay *map[uint8]*[NumOfCardsOnePlayer]uint8) {
	tqs := &tableRequest{
		topic: _GetZoneUsers,
	}

	rep := mr.table.Probe(tqs)
	if rep.err != nil {
		slog.Error("發牌SendDeal錯誤", utilog.Err(rep.err))
	}
	//玩家發牌
	mr.sendDealToPlayer(deckInPlay, rep.e.NsConn, rep.s.NsConn, rep.w.NsConn, rep.n.NsConn)

	//觀眾發牌
	mr.sendDealToZone(deckInPlay, rep.audiences.Connections())
}

// send 針對payload型態對連線發送 []byte 或 proto bytes
func (mr *RoomManager) send(nsConn *skf.NSConn, payload payloadData, eventName string) error {

	if nsConn == nil || nsConn.Conn.IsClosed() {
		return errors.New(fmt.Sprintf("%s Zone/Player 連線為nil或斷線,payload型態: %d", CbSeat(payload.Player), payload.PayloadType))
	}

	switch payload.PayloadType {
	case ByteType:
		nsConn.EmitBinary(eventName, payload.Data)
	case ProtobufType:
		marshal, err := pb.Marshal(payload.ProtoData)
		if err != nil {
			return err
		}
		nsConn.EmitBinary(eventName, marshal)
	}
	return nil
}

// SendPayloads 針對某個Player(玩家)發送多筆訊息,或一筆訊息
func (mr *RoomManager) SendPayloads(eventName string, payloads ...payloadData) {

	if len(payloads) == 0 {
		panic("SendPayloads 屬性player必須要有值(seat)")
	}

	tps := &tableRequest{
		topic:  _FindPlayer,
		player: &RoomUser{Zone8: payloads[0].Player}, /*[0]:第一個樣本*/
	}
	rep := mr.table.Probe(tps)
	if rep.err != nil {
		slog.Error("找尋玩家連線失敗(SendPayloads)", utilog.Err(rep.err))
		return
	}

	for i := range payloads {
		err := mr.send(rep.player, payloads[i], eventName)
		if err != nil {
			slog.Error("payload發送失敗(SendPayloads)", utilog.Err(err))
			continue
		}
	}
}

// SendPayloadToPlayers 同時對4座玩家發送一則訊息(payload)
func (mr *RoomManager) SendPayloadToPlayers(payloads []payloadData, eventName string) {

	var (
		err          error
		errFmtString = "%s玩家連線中斷"
		connections  = make(map[uint8]*skf.NSConn)
	)

	connections[east], connections[south], connections[west], connections[north] = mr.acquirePlayerConnections()

	if connections[east] == nil {
		err = fmt.Errorf(errFmtString, "east")
	}
	if connections[south] == nil {
		err = fmt.Errorf(errFmtString, "north")
	}
	if connections[west] == nil {
		err = fmt.Errorf(errFmtString, "west")
	}
	if connections[north] == nil {
		err = fmt.Errorf(errFmtString, "north")
	}

	if err != nil {
		slog.Error("連線中斷(SendPayloadToPlayers)", utilog.Err(err))
		//TODO 對未斷線玩家,送出現在狀況,好讓前端popup
		for _, nsConn := range connections {
			if nsConn != nil {
				nsConn.EmitBinary("popup-warning", []byte(err.Error()))
			}
		}

	} else {
		for i := range payloads {
			err = mr.send(connections[payloads[i].Player], payloads[i], eventName)
			if err != nil {
				slog.Error("payload發送失敗(SendPayloadToPlayers)", utilog.Err(err))
				continue
			}
		}
	}

}

// SendPayloadsToZone 針對觀眾(不包含任何玩家)發送訊息,
func (mr *RoomManager) SendPayloadsToZone(payloads []payloadData, eventName string) {
	tqs := &tableRequest{
		topic: _GetZoneUsers,
	}
	rep := mr.table.Probe(tqs)
	if rep.err != nil {
		slog.Error("發送訊息錯誤(SendPayloadsToZone)", utilog.Err(rep.err))
	}

	var err error

	connections := rep.audiences.Connections()

	for i := range connections {
		for j := range payloads {
			if err = mr.send(connections[i], payloads[j], eventName); err != nil {
				slog.Error("payload發送失敗(SendPayloadsToZone)", utilog.Err(err))
			}
		}
	}
}

//BroadcastXXXX 用於廣播,無論玩家,觀眾都會同時送出同樣的訊息,通常用於公告,聊天資訊, 遊戲共同訊息(叫牌)
/* ============================================================================================
                               SendXXXX 指資訊個別的送出給玩家,觀眾通常用於遊戲資訊
======================== ====================================================================== */

func (mr *RoomManager) roomDebugDump() {
	//Total: 每個Zone人數相加
	eastZone := len(mr.Users[playerSeats[0]])
	southZone := len(mr.Users[playerSeats[1]])
	westZone := len(mr.Users[playerSeats[2]])
	northZone := len(mr.Users[playerSeats[3]])
	total := eastZone + southZone + westZone + northZone
	slog.Info("房間資訊",
		slog.Int("East人數", eastZone),
		slog.Int("South人數", southZone),
		slog.Int("West人數", westZone),
		slog.Int("North人數", northZone),
		slog.Int("房間總人數", total))
}

// broadcast 房間,若發生問題,AppErr.Code可能是BroadcastC,若全部的人都不能訊息發送屬於嚴重錯誤就會是(NSConnC),AppErr.reason則會是發送失敗的人
func (mr *RoomManager) broadcast(b *broadcastRequest) (err AppErr) {

	isSkip := b.sender != nil && !b.sender.Conn.IsClosed()

	var appErr = AppErr{Code: AppCodeZero} //設定初值(zero value)

	//失敗送出的使用者(含觀眾與玩家)
	fails := make([]*RoomUser, 0, RoomUsersLimit)

	// roomUsers用來判斷全部發送錯誤還是部份發送錯誤
	roomUsers := int(0)

	for _, zone := range playerSeats {
		for Ns, user := range mr.Users[zone] {

			//略過發言訊息者
			if isSkip && b.sender == Ns {
				continue
			}

			//判斷是全部發送錯誤還是部份發送錯誤
			roomUsers++

			//略過已斷線玩家
			if Ns.Conn.IsClosed() {
				fails = append(fails, user)
				appErr.Code = BroadcastC
				continue
			}
			// 寫出
			if ok := Ns.Conn.Write(*b.msg); !ok {
				//紀錄失敗送出, 並處理這個 user
				//TODO
				fails = append(fails, user)
				appErr.Code = BroadcastC
				continue
			}
		}
	}

	if appErr.Code != AppCodeZero {
		appErr.Msg = "連線出錯,聊天訊息送出失敗"
		//發送次數與失敗人數一樣,表示全部發送錯誤
		if roomUsers == len(fails) {
			appErr.Err = errors.New("廣播連線全部掛掉")
			appErr.Code = NSConnC | appErr.Code
		}
	}

	appErr.reason = fails
	return
}

// broadcastMsg 這是獨立的方法不是 RoomManager的屬性,將傳入參數生成 skf.Message
func broadcastMsg(sender *skf.NSConn, eventName, roomName string, serializedBody []uint8, errInfo error) (msg *skf.Message) {
	//sender sender不為nil情況下只會發生在傳送聊天訊息時,通常sender會是nil
	// roomName送到那個Room (TBC 要與前端確認)
	// serializedBody 發送的封包
	// errInfo 發送給前端必須處理的錯誤訊息
	var from string
	if sender != nil {
		//TODO : 不應該是 sender.String(), 應該是 RoomUser.Name
		from = sender.String()
	}

	msg = new(skf.Message)
	msg.Namespace = RoomSpaceName
	msg.Room = roomName
	msg.Event = eventName
	msg.Body = serializedBody
	msg.SetBinary = true
	msg.FromExplicit = from
	msg.Err = errInfo
	return
}

// BroadcastChat 除了發送者外,所有的人都會被廣播, 用於聊天室聊天訊息
func (mr *RoomManager) BroadcastChat(sender *skf.NSConn, eventName, roomName string, serializedBody []uint8 /*body*/, errInfo error /*告訴Client有錯誤狀況發生*/) {
	// sender 送出聊天訊息的連線  eventName 事件名(TODO: 常數值)
	// roomName送到那個Room (TBC 要與前端確認)
	// serializedBody 發送的封包
	// errInfo 發送給前端必須處理的錯誤訊息
	b := &broadcastRequest{
		msg:    broadcastMsg(sender, eventName, roomName, serializedBody, errInfo),
		sender: sender,
		to:     nil,
		chat:   true,
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastChat")
}

// BroadcastBytes 發送 []uint8 封包給所有人
func (mr *RoomManager) BroadcastBytes(eventName, roomName string, serializedBody []uint8) {
	b := &broadcastRequest{
		msg: broadcastMsg(nil, eventName, roomName, serializedBody, nil),
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastBytes")
}

// BroadcastByte 發送 uint8 給所有人
func (mr *RoomManager) BroadcastByte(eventName, roomName string, body uint8) {
	b := &broadcastRequest{
		msg: broadcastMsg(nil, eventName, roomName, []byte{body}, nil),
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastByte")
}

// BroadcastString 發送字串內容給所有人
func (mr *RoomManager) BroadcastString(eventName, roomName string, body string) {
	b := &broadcastRequest{
		msg: broadcastMsg(nil, eventName, roomName, []byte(body), nil),
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastString")
}

// BroadcastProtobuf 發送protobuf 給所有人
func (mr *RoomManager) BroadcastProtobuf(eventName, roomName string, body proto.Message) {
	marshal, err := pb.Marshal(body)
	if err != nil {
		slog.Error("ProtoMarshal(BroadcastProtobuf)", utilog.Err(err))
		return
	}

	b := &broadcastRequest{
		msg: broadcastMsg(nil, eventName, roomName, marshal, nil),
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastProtobuf")
}

// DevelopBroadcastTest user用於測試 BroadcastChat
func (mr *RoomManager) DevelopBroadcastTest(user *RoomUser) {
	//byte
	payloads := []uint8{east}
	roomName := "room0x0"
	mr.BroadcastBytes(ClnRoomEvents.DevelopBroadcastTest, roomName, payloads)

	//bytes
	payloads = append(payloads, south, west, north)
	m.BroadcastBytes(ClnRoomEvents.DevelopBroadcastTest, roomName, payloads)

	//string

	//protobuf
}

func (mr *RoomManager) DevelopPrivatePayloadTest(user *RoomUser) {
	fmt.Println("[DevelopPrivatePayloadTest]")
	eventName := ClnRoomEvents.DevelopPrivatePayloadTest

	p := payloadData{}
	//case1 byte ,前端判斷 msg.value 只要不為null, 就可取出byte值
	p.PayloadType = ByteType
	p.Data = []byte{east}
	p.Player = east
	p.ProtoData = nil
	mr.send(user.NsConn, p, eventName) // 👍

	//case2 bytes ,前端判斷 msg.values 只要不為null, 就可取出bytes值
	/*	p.PayloadType = ByteType
		p.PayloadType = ByteType
		p.Data = append(p.Data, south, west, north)
		p.Player = east
		p.ProtoData = nil
		mr.send(user.NsConn, p, eventName)
	*/
	//case3 proto ,前端判斷 msg.pbody只要不為null, 就可取出pbody(protobuf)值
	p.PayloadType = ProtobufType
	message := pb.MessagePacket{
		Type:    pb.MessagePacket_Admin,
		Content: "hello MessagePacket",
		Tt:      pb.LocalTimestamp(time.Now()),
		RoomId:  12,
		From:    "Server",
		To:      "Client",
	}
	anyItem, err := anypb.New(&message)
	if err != nil {
		panic(err)
	}

	packet := pb.ProtoPacket{
		AnyItem: anyItem,
		Tt:      pb.LocalTimestamp(time.Now()),
		Topic:   pb.TopicType_Message,
		SN:      99,
	}
	p.ProtoData = &packet
	mr.send(user.NsConn, p, eventName) // 👍

	//case4 String ,前端判斷 msg.body只要不為null, 就可取出string值
	p.PayloadType = ByteType
	p.Data = p.Data[:]
	p.Data = []uint8("人間にんげん")
	mr.send(user.NsConn, p, eventName) // 👍
}

// 檢驗BroadcastXXXX後的結果,並log錯誤
func checkBroadcastError(probe AppErr, broadcastName string) {
	if probe.Code != AppCodeZero {
		errorSubject := fmt.Sprintf("訊息送出失敗(%s)", broadcastName)
		switch probe.Code {
		case BroadcastC | NSConnC:
			slog.Error("嚴重錯誤(BroadcastChat)", utilog.Err(probe.Err))
			fallthrough
			//TODO log here
		default: /*BroadcastC*/
			slog.Error(errorSubject, slog.String("msg", probe.Msg))
			fails := probe.reason.([]*RoomUser)
			var fail *RoomUser
			for i := range fails {
				fail = fails[i]
				slog.Error(" 錯誤資訊", slog.String("RoomUser", fail.Name), slog.String("區域", fmt.Sprintf("%s", CbSeat(fail.Zone))), slog.Any("連線", fail.NsConn))
			}
		}
	}
}
