package game

import (
	"container/ring"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/moszorn/pb"
	utilog "github.com/moszorn/utils/log"
	"github.com/moszorn/utils/rchanr"
	"github.com/moszorn/utils/skf"
	"project"
)

// RoomManager 管理進入房間的所有使用者,包含廣播所有房間使用者,發送訊息給指定玩家
// 未來可能會分方位(RoomZorn),禁言,聊天可能都透過RoomManager
type (
	// table 操作或請求
	tableRequest struct {
		topic      tableTopic
		user       *RoomUser /* IsPlayerOnSeat, EnterGame, LeaveGame, */
		player     *RoomUser
		shiftSeat  uint8 /* SeatShift */
		actionSeat uint8 /* PlayerAction */
	}

	//執行結果
	chanResult struct {
		err error

		e *RoomUser //east 玩家
		w *RoomUser //west 玩家
		s *RoomUser //south 玩家
		n *RoomUser //north 玩家

		// 代表所有Zone的觀眾連線資料結構,不含Player連線
		zoneUsers []*skf.NSConn

		//代表一個玩家的連線
		player *skf.NSConn

		seat        uint8
		isGameStart bool

		//表示遊戲已經幾人動作了(回合數)
		aa uint8

		//玩家是否已入座
		isOnSeat bool
	}

	broadcastRequest struct {
		msg    *skf.Message
		sender *skf.NSConn // 訊息發送者 , sender != nil 表示聊天, sender == nil 表示管理,公告訊息
		to     *skf.NSConn // (預留)私人訊息發送 , to != nil 表示私訊

		//chat 與 admin同時 false 表示一般訊息發送
		chat  bool // 聊天訊息 chat = true 訊息分(私人,公開)所以需要再判斷 sender, to
		admin bool // 管理,公告訊息, 除了admin,chat 同為false是允許的外, admin 與 chat 是互斥的也就不會有 chat = true, admin = true
	}

	// tablePlayer就是seatItem
	tablePlayer struct {
		player *RoomUser
		zone   uint8 //代表player座位(CbSeat)東南西北,每個SeatItem初始化時必須指派一個不能重覆的位置
		value  uint8 //當前打出什麼牌(Card)
	}

	ZoneUsers map[*skf.NSConn]*RoomUser

	RoomZoneUsers map[uint8]ZoneUsers

	RoomManager struct {
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

	}
)

// NewRoomManager RoomManager建構子
func NewRoomManager() *RoomManager {
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
			zone:   playerSeats[i],
			player: new(RoomUser),
		}
	}
	var mr *RoomManager = new(RoomManager)
	mr.Users = roomZoneUsers
	mr.door = make(chan rchanr.ChanRepWithArguments[*RoomUser, chanResult])
	mr.table = make(chan rchanr.ChanRepWithArguments[*tableRequest, chanResult])
	mr.broadcastMsg = make(chan rchanr.ChanRepWithArguments[*broadcastRequest, AppErr])
	mr.Ring = r
	go mr.roomManagerStartLoop()
	return mr
}

// roomManagerStartLoop RoomManager開始幹活
func (mr *RoomManager) roomManagerStartLoop() {
	for {
		select {
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
					mr.Users[user.Zone][user.NsConn] = user
					result.err = nil
					result.isGameStart = mr.players >= 4
				}
				tracking.Response <- result
			case LeaveRoom:

				// 離開玩家

				delete(mr.Users[user.Zone], user.NsConn)

				//TBC 底下一行離開房間,到大廳,是否需要斷線
				// user.NsConn = nil

				//房間進入者流水編號遞減
				mr.ticketSN--

				user = nil

				tracking.Response <- chanResult{
					err:         nil,
					isGameStart: mr.players >= 4,
				}
			case EnterGame:
				//外界在呼叫 EnterGame前,要先判斷遊戲是否開始,玩家是否已經入桌
				seat, gameStart := mr.playerJoin(user, pb.SeatStatus_SitDown)
				result := chanResult{}
				result.seat = seat /*zoneSeat valueNotSet 表桌已滿,並且gameStart會是 true*/
				result.isGameStart = gameStart
				result.err = nil
				tracking.Response <- result
			case LeaveGame:
				seat, gameStart := mr.playerJoin(user, pb.SeatStatus_StandUp)
				result := chanResult{}
				result.seat = seat
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
				result.err = nil
				result.isGameStart = mr.players >= 4
				result.aa = mr.aa
				crwa.Response <- result

			case PlayerAction:

				//打出一張牌, 這裡應該還要再回傳
				//1. 出牌儲牌否
				//3. 是否一回合,是否最後一張
				//2. 四家牌面 seatPlays() ,
				//某些條件成立時,執行 resetPlay 動作, seatShifging

				result := chanResult{}

				if mr.aa >= 4 {
					result.seat = req.player.Zone //user.Player
					result.aa = mr.aa
					result.err = nil
					crwa.Response <- result
					// 注意: break 會直接下一個循環,因此break後面會被忽略
					break
				}

				if !mr.savePlayerCardValue(req.player) {
					result.err = errors.New("座位打出的牌有誤")
					result.seat = req.player.Zone
					result.aa = mr.aa
					result.isGameStart = mr.players >= 4
					result.err = nil
					crwa.Response <- result
					// 注意: break 會直接下一個循環,因此break後面會被忽略
					break
				}

				mr.aa++
				result.seat = req.player.Zone
				result.err = nil
				result.aa = mr.aa
				crwa.Response <- result
			case _FindPlayer:

				result := chanResult{}
				result.isGameStart = mr.players >= 4
				result.aa = mr.aa
				result.err = nil

				var ringItem *tablePlayer
				ringItem, result.isOnSeat = mr.findPlayer(req.player.Zone)

				if result.isOnSeat {
					//找到指定玩家連線
					result.player = ringItem.player.NsConn
				}
				crwa.Response <- result

			case _GetTablePlayers:
				result := chanResult{}
				result.e, result.s, result.w, result.n = mr.tablePlayers()
				result.err = nil
				crwa.Response <- result
			case _GetZoneUsers:
				//撈取 Player Block連線
				result := chanResult{}
				result.err = nil
				result.aa = mr.aa
				result.isGameStart = mr.players >= 4
				result.zoneUsers, result.e, result.s, result.w, result.n = mr.zoneUsers()

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

// UserJoin 使用者進入房間, 也是 NSConn變成 RoomUser的起始點
func (mr *RoomManager) UserJoin(ns *skf.NSConn, userName string, userZone uint8) (chanSeat *chanResult) {
	// ns 進入房間的連線  userName 進入房間的使用者名稱   userZone 使用者由那一個Zone(east,south,west,north)進入房間

	user := &RoomUser{
		NsConn:     ns,
		Name:       userName,
		TicketTime: time.Time{},
		Tracking:   IddleTrack,
		Zone:       userZone,
	}
	//TBC 好像 Tracking只用來當成 switch的判斷,不需要使用 preTracking 這個機制
	// TODO 移除 preTracking
	preTracking := user.Tracking
	user.Tracking = EnterRoom

	var response chanResult
	//Probe內部用user name查詢是否user已經入房間
	response = mr.door.Probe(user)

	// 房間已滿(超出RoomUsersLimit), 或使用者已存在房間
	if chanSeat.err != nil {
		//TODO 移除 Tracking還原
		user.Tracking = preTracking
		slog.Debug("使用者進入房間", utilog.Err(chanSeat.err))
		return &response
	}

	//成功
	return &response
}

// UserLeave 使用者離開房間
func (mr *RoomManager) UserLeave(ns *skf.NSConn, userName string, userZone uint8) (chanSeat *chanResult) {
	//	ns 使用者連線, userName玩家名稱, userZone 玩家進入房間時的方位Zone

	user := &RoomUser{
		NsConn:     ns,
		Name:       userName,
		TicketTime: time.Time{},
		Tracking:   0,
		Zone:       userZone,
	}
	//TBC 好像 Tracking只用來當成 switch的判斷,不需要使用 preTracking 這個機制
	// TODO 移除 preTracking
	preTracking := user.Tracking
	user.Tracking = LeaveRoom

	response := mr.door.Probe(user)

	if response.err != nil {
		//TODO 移除 Tracking還原
		user.Tracking = preTracking
	}
	return &response
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

	zoneSeat = valueNotSet
	var seatAt *tablePlayer
	for i := 0; i < PlayersLimit; i++ {
		seatAt = mr.Value.(*tablePlayer)
		mr.Ring = mr.Next()
		switch flag {
		case pb.SeatStatus_SitDown:
			//有空位
			if seatAt.player.NsConn == nil {

				//注意用copy的
				seatAt.player.NsConn = user.NsConn
				seatAt.player.TicketTime = user.TicketTime

				zoneSeat = seatAt.zone // 入座
				user.Tracking = EnterGame
				mr.players++
				//回傳的zoneSeat不可能是 0x0
				return zoneSeat, mr.players >= 4
			}
		case pb.SeatStatus_StandUp:
			if seatAt.player.NsConn != nil && seatAt.player.NsConn == user.NsConn {
				seatAt.player.NsConn = nil // 離座
				seatAt.player.Play = valueNotSet
				seatAt.player.Bid = valueNotSet
				seatAt.player.Name = ""

				zoneSeat = seatAt.zone // 離那個座
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
	if found, exist := mr.findPlayer(player.Zone); exist {
		if found.player.NsConn == player.NsConn {
			found.value = player.Play
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
func (mr *RoomManager) zoneUsers() (users []*skf.NSConn, ePlayer, sPlayer, wPlayer, nPlayer *RoomUser) {
	// users 表示所有觀眾使用者連線, 東南西北玩家(player)分別是 ePlayer, sPlayer, wPlayer, nPlayer

	//玩家連線
	ePlayer, sPlayer, wPlayer, nPlayer = mr.tablePlayers()

	//觀眾連線
	users = make([]*skf.NSConn, 0, len(mr.Users)-4) //-4 扣除四位玩家

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
		for conn := range mr.Users[zone] {
			if !conn.Conn.IsClosed() && conn != player {
				users = append(users, conn)
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
func (mr *RoomManager) SeatShift(seat uint8) uint8 {
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
				project.ClnRoomEvents.GamePrivateRoundStartToDeal,
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
		users[i].EmitBinary(project.ClnRoomEvents.GamePrivateRoundStartToDeal, eHand)
		users[i].EmitBinary(project.ClnRoomEvents.GamePrivateRoundStartToDeal, sHand)
		users[i].EmitBinary(project.ClnRoomEvents.GamePrivateRoundStartToDeal, wHand)
		users[i].EmitBinary(project.ClnRoomEvents.GamePrivateRoundStartToDeal, nHand)
	}
}

// SendDeal 向玩家, 觀眾(Player)發牌
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
	mr.sendDealToZone(deckInPlay, rep.zoneUsers)
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

// SendPayloads 針對某個Player發送多筆訊息,或一筆訊息
func (mr *RoomManager) SendPayloads(payloads []payloadData, eventName string) {

	tps := &tableRequest{
		topic:  _FindPlayer,
		player: &RoomUser{Zone: payloads[0].Player},
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

// SendPayloadsToZone 針對觀眾(不包含任何玩家)發送訊息
func (mr *RoomManager) SendPayloadsToZone(payloads []payloadData, eventName string) {
	tqs := &tableRequest{
		topic: _GetZoneUsers,
	}
	rep := mr.table.Probe(tqs)
	if rep.err != nil {
		slog.Error("發送訊息錯誤(SendPayloadsToZone)", utilog.Err(rep.err))
	}

	var err error
	for i := range rep.zoneUsers {
		for j := range payloads {
			if err = mr.send(rep.zoneUsers[i], payloads[j], eventName); err != nil {
				slog.Error("payload發送失敗(SendPayloadsToZone)", utilog.Err(err))
			}
		}
	}
}

//BroadcastXXXX 用於廣播,無論玩家,觀眾都會同時送出同樣的訊息,通常用於公告,聊天資訊, 遊戲共同訊息(叫牌)
/* ============================================================================================
                               SendXXXX 指資訊個別的送出給玩家,觀眾通常用於遊戲資訊
======================== ====================================================================== */

func (mr *RoomManager) roomInfo() {
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

// broadcast 房間,遊戲廣播, 若發生問題,AppErr.Code 必定是 NSConn, AppErr.reason會是 []*RoomUser
func (mr *RoomManager) broadcast(b *broadcastRequest) (err AppErr) {

	isSkip := b.sender != nil && !b.sender.Conn.IsClosed()

	var appErr = AppErr{Code: AppCodeZero}

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

// broadcastMsg 這是獨立的方法不是 RoomManager的屬性,將傳入的生成skf.Message
func broadcastMsg(sender *skf.NSConn, eventName, roomName string, serializedBody []uint8, errInfo error) (msg *skf.Message) {

	var from string
	if sender != nil {
		//TODO : 不應該是 sender.String(), 應該是 RoomUser.Name
		from = sender.String()
	}

	msg = new(skf.Message)
	msg.Namespace = project.RoomSpaceName
	msg.Room = roomName
	msg.Event = eventName
	msg.Body = serializedBody
	msg.SetBinary = true
	msg.FromExplicit = from
	msg.Err = errInfo
	return
}

// BroadcastChat 除了發送者外,所有的都會廣播, 用於聊天室聊天訊息
func (mr *RoomManager) BroadcastChat(sender *skf.NSConn, eventName, roomName string, serializedBody []uint8 /*body*/, errInfo error /*告訴Client有錯誤狀況發生*/) {
	b := &broadcastRequest{
		msg:    broadcastMsg(sender, eventName, roomName, serializedBody, errInfo),
		sender: sender,
		to:     nil,
		chat:   true,
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastChat")
}

func (mr *RoomManager) BroadcastBytes(eventName, roomName string, serializedBody []uint8) {
	b := &broadcastRequest{
		msg: broadcastMsg(nil, eventName, roomName, serializedBody, nil),
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastBytes")
}
func (mr *RoomManager) BroadcastByte(eventName, roomName string, body uint8) {
	b := &broadcastRequest{
		msg: broadcastMsg(nil, eventName, roomName, []byte{body}, nil),
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastByte")
}
func (mr *RoomManager) BroadcastString(eventName, roomName string, body string) {
	b := &broadcastRequest{
		msg: broadcastMsg(nil, eventName, roomName, []byte(body), nil),
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastString")
}
func (mr *RoomManager) BroadcastProtobuf(eventName, roomName string, body pb.Message) {
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
