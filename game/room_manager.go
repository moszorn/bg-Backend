package game

import (
	"container/ring"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/moszorn/pb/cb"
	//"github.com/moszorn/pb/cb"
	//"github.com/moszorn/pb/cb"
	"google.golang.org/protobuf/proto"
	//"google.golang.org/protobuf/types/known/anypb"

	"github.com/moszorn/pb"
	//utilog "github.com/moszorn/utils/log"
	"github.com/moszorn/utils/rchanr"
	"github.com/moszorn/utils/skf"
)

var (
	shortConnID = func(c *skf.NSConn) string {
		var (
			index = strings.LastIndex(c.String(), "-")
			id    = c.String()[index+1:]
		)
		if c.Conn.IsClosed() {
			return "斷 ⛓️ 線 👉🏼" + id
		}
		return id
	}
)

const (
	ByteType PayloadType = iota
	ProtobufType
)

// RoomManager 管理進入房間的所有使用者,包含廣播所有房間使用者,發送訊息給指定玩家
// 未來可能會分方位(RoomZorn),禁言,聊天可能都透過RoomManager
type (
	PayloadType uint8

	payloadData struct {
		Player      uint8         //代表player seat 通常針對指定的玩家, 表示Zone的情境應該不會發生
		Data        []uint8       // 可以是byte, bytes
		ProtoData   proto.Message // proto
		PayloadType PayloadType   //這個 payload 屬於那種型態的封	包
	}

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

		alives [3]*skf.NSConn //代表仍未斷線離開遊戲桌的三位玩家

		// 代表所有Zone的觀眾連線資料結構,不含Player連線
		audiences Audiences
		// 代表以空位為始點的環形元素陣列
		seatOrders [4]*RoomUser

		//代表一個玩家的連線
		player *skf.NSConn
		//代表玩家名稱
		playerName string

		seat        uint8
		isGameStart bool

		//表示遊戲已經幾人動作了(回合數)
		aa uint8

		//玩家是否入座
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
				PlayingUser: &pb.PlayingUser{Zone: uint32(playerSeats[i])},
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
			switch tracking.Question.Tracking {
			case EnterRoom:
				user := tracking.Question
				result := chanResult{}
				//Zorn ============================
				if _, exist := mr.getRoomUser(user.NsConn); exist {
					result.err = ErrUserInRoom
				}

				if mr.ticketSN > RoomUsersLimit {
					result.err = ErrRoomFull
				}
				if user.Zone8 == valueNotSet {
					slog.Error("RoomManager(Loop-EnterRoom)", slog.String(".", fmt.Sprintf("%s(%d) %s 進入房間方位(%[1]s)不存在", CbSeat(user.Zone8), user.Zone8, user.Name)))
				} else {
					user.Ticket()
					//房間進入者流水編號累增
					mr.ticketSN++

					// 玩家加入遊戲房間
					mr.Users[user.Zone8][user.NsConn] = user
					result.playerName = user.Name
					result.err = nil //成功入房
					result.isGameStart = mr.players >= 4
				}
				tracking.Response <- result
			case LeaveRoom:
				user := tracking.Question
				var leaverName string
				if zone, ok := mr.Users[user.Zone8]; ok {
					if roomUser, ok := zone[user.NsConn]; ok {
						slog.Debug("RoomManager(Loop-LeaveRoom)", slog.String("移出房間", roomUser.Name))
						leaverName = roomUser.Name
						delete(zone, user.NsConn)

						//房間進入者流水編號遞減
						mr.ticketSN--

					}
				} else {
					slog.Error("RoomManager(Loop-LeaveRoom)", slog.String(".", fmt.Sprintf("zone:%s(%d) %s不在房間任何zone中", CbSeat(user.Zone8), user.Zone8, user.Name)))
				}

				//為何這裡需要將設定user為nil,是因要釋放在UserLeave時的記憶體參考
				user = nil
				tracking.Response <- chanResult{
					err:         nil,
					isGameStart: mr.players >= 4,
					playerName:  leaverName,
				}
			case EnterGame:
				user := tracking.Question
				allowEnterGame := true
				result := chanResult{}

				//檢查 --------------------
				audiences, ePlayer, sPlayer, wPlayer, nPlayer := mr.zoneUsers()

				// 檢查進入者是否已在遊戲中,有=> 回復錯誤
				switch user.NsConn {
				case ePlayer.NsConn:
					fallthrough
				case sPlayer.NsConn:
					fallthrough
				case wPlayer.NsConn:
					fallthrough
				case nPlayer.NsConn:
					allowEnterGame = false
				}
				// 返回
				if !allowEnterGame {
					//進入者已在遊戲中
					//返回
					result.err = ErrUserInPlay
					tracking.Response <- result
					continue
				}

				//判斷自房間否
				allowEnterGame = false
				//檢查進入者有否在桌中,不在桌中=>回復錯誤
				for i := range audiences {
					if !audiences[i].NsConn.Conn.IsClosed() &&
						audiences[i].Name == user.Name &&
						audiences[i].Zone8 == user.Zone8 &&
						audiences[i].NsConn == user.NsConn {
						//進入者已經在房間在房間
						allowEnterGame = true
					}
				}

				if !allowEnterGame {
					//進入者尚未進入房間中
					result.err = ErrUserNotFound
					//返回
					tracking.Response <- result
					continue
				}

				// 未來 檢查進入者是否已在其站上其它房間遊戲中 (by Dynamodb)
				//result.err = ErrPlayMultipleGame //同時多局遊戲

				//進入遊戲-----------------------
				/*
				 result.seat 表示入座位置
				 result.playerName 表示入座者姓名
				*/
				result.seat, result.playerName, result.isGameStart = mr.playerJoin(user, pb.SeatStatus_SitDown)
				result.isOnSeat = result.seat != valueNotSet
				result.err = nil
				tracking.Response <- result

			case LeaveGame:
				user := tracking.Question
				/*
				 result.seat 表示離座位置
				 result.playerName 表示離座者姓名
				*/
				result := chanResult{}
				result.seat, result.playerName, result.isGameStart = mr.playerJoin(user, pb.SeatStatus_StandUp)
				//通知三位玩家
				result.alives[0],
					result.alives[1],
					result.alives[2] = mr.acquirePlayerConnectionsByExclude(user.Zone8)

				result.err = nil
				tracking.Response <- result

			}

		case crwa := <-mr.table:
			req := crwa.Question
			switch req.topic {
			case IsPlayerOnSeat:
				/*
				 result.seat 表示玩家座位
				 result.isOnSeat 表示玩家是否遊戲中
				 result.playerName 表示玩家姓名
				*/
				result := chanResult{}

				found := false
				limit := PlayersLimit
				seat := mr.Value.(*tablePlayer)
				for limit > 0 && !found {
					limit--
					if seat.player != nil && seat.player == req.user {
						found = true
						result.seat = seat.zone
					}
					mr.Ring = mr.Next()
					seat = mr.Value.(*tablePlayer)
				}

				if found { //表存已在遊戲中
					result.isOnSeat = true
				} else {
					result.isOnSeat = false
				}
				result.playerName = req.player.Name
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

				if ringItem == nil || !result.isOnSeat {
					result.err = errors.New(fmt.Sprintf("(%s)已離開/斷線", CbSeat(req.player.Zone8)))
				} else {
					/*
						slog.Debug("RoomManager(Loop-_FindPlayer)",
							slog.String("姓名", ringItem.player.Name),
							slog.String("座位(Zone8)", fmt.Sprintf("%s", CbSeat(ringItem.player.Zone8))),
							slog.Int("seat(zone)", int(ringItem.zone)),
						)*/
					//不管isOnSeat有否在座位上,都登記尋找的玩家名稱
					result.playerName = ringItem.player.Name
					if result.isOnSeat {
						//找到指定玩家連線
						result.player = ringItem.player.NsConn
					}
					result.err = nil
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
func (mr *RoomManager) getZoneRoomUser(nsConn *skf.NSConn, zone uint8) (found *RoomUser, isExist bool) {
	found, isExist = mr.Users[zone][nsConn]
	return
}

// KickOutBrokenConnection 不正常連線(斷線)踢出房間與遊戲, zone若為
func (mr *RoomManager) KickOutBrokenConnection(ns *skf.NSConn) {

	var (
		roomName   string = ns.Conn.Get(KeyRoom).(string)
		kickZone   uint8  = ns.Conn.Get(KeyZone).(uint8)
		kickInGame bool   = ns.Conn.Get(KeyGame) != nil
	)

	slog.Debug("KickOutBrokenConnectionFromRoom",
		slog.String(fmt.Sprintf("連線:%s", shortConnID(ns)),
			fmt.Sprintf("區域:%s 遊戲中:%t 遊戲間:%s", CbSeat(kickZone), kickInGame, roomName)))

	kick := &RoomUser{
		NsConn: ns,
		PlayingUser: &pb.PlayingUser{
			Zone:      uint32(kickZone),
			IsSitting: kickInGame,
		},
		Zone8:          kickZone,
		IsClientBroken: true,
	}

	if kickInGame {
		mr.PlayerLeave(kick)
	}
	mr.UserLeave(kick)

	/*
		kick := &RoomUser{
			NsConn:   ns,
			Tracking: LeaveGame,
			Zone8:    zone,
			PlayingUser: &pb.PlayingUser{
				IsSitting: gameKickOut,
			},
		}
		mr.UserLeave(kick)
	*/
}

// UserJoinTableInfo 房間人數,桌中座位順序與座位狀態, 使用者進入房間時需要此資訊
func (mr *RoomManager) UserJoinTableInfo(user *RoomUser) {

	slog.Info("UserJoinTableInfo", slog.String("傳入參數", fmt.Sprintf("name:%s conn:%s", user.Name, shortConnID(user.NsConn))))

	tqs := &tableRequest{
		topic: _GetTableInfo,
	}

	rep := mr.table.Probe(tqs)
	if rep.err != nil {
		slog.Error("UserJoinTableInfo錯誤", slog.String(".", rep.err.Error()))
	}

	var pp = pb.TableInfo{
		/*底下是該桌組態設定*/
		CountDown: GamePlayCountDown,
	}

	//觀眾資訊(房間中的人):包含沒在座位上的與在座位上的
	pp.Audiences = make([]*pb.PlayingUser, 0, len(rep.audiences)+PlayersLimit)

	//有順序的四個座位資訊(從第一個空位開始)
	pp.Players = make([]*pb.PlayingUser, 0, PlayersLimit)

	for i := range rep.seatOrders {
		//填充座位空位順序
		pp.Players = append(pp.Players, rep.seatOrders[i].PlayingUser)

		//填充觀眾資訊-之座位上的玩家
		if rep.seatOrders[i].PlayingUser.Name != "" {
			pp.Audiences = append(pp.Audiences, rep.seatOrders[i].PlayingUser)
		}
	}

	for i := range rep.audiences {
		//填充觀眾資訊-之沒在座位上的觀眾
		pp.Audiences = append(pp.Audiences, rep.audiences[i].PlayingUser)
	}

	//最後將新進房間的使用者也加入觀眾席
	pp.Audiences = append(pp.Audiences, user.PlayingUser)

	payload := payloadData{
		ProtoData:   &pp,
		PayloadType: ProtobufType,
	}

	if err := mr.send(user.NsConn, ClnRoomEvents.UserPrivateTableInfo, payload); err != nil {
		slog.Error("UserJoinTableInfo proto錯誤", slog.String(".", err.Error()))
	}
}

// UserJoin 使用者進入房間, 必須參數RoomUser {*skf.NSConn, userName, userZone}
func (mr *RoomManager) UserJoin(user *RoomUser) {
	// UserJoin 姓名="" user.Zone8=東家 ""=東家
	slog.Info("UserJoin-進入房間", slog.String("姓名", user.PlayingUser.Name), slog.Bool("入座", user.IsSitting), slog.String("zone8", fmt.Sprintf("%s(%d)", CbSeat(user.Zone8), user.Zone)))

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
		slog.Debug("使用者進入房間(UserJoin)", slog.String(".", response.err.Error()))
		if user.NsConn != nil && !user.NsConn.Conn.IsClosed() {
			user.NsConn.Emit(ClnRoomEvents.ErrorRoom, []byte(response.err.Error()))
		}
		user = nil
		return
	}

	//使用者不正常斷線離開時,KeyInRoomStatus可以用來判斷
	// 設定KeyRoom表示進入房間,這也表示也者定了進入房間的Zone (KeyZone)
	user.NsConn.Conn.Set(KeyRoom, mr.g.name)  //表示進入房間
	user.NsConn.Conn.Set(KeyZone, user.Zone8) //表示進入哪個區

	mr.g.CounterAdd(user.NsConn, mr.g.name)

	//廣播房間有人進入房間
	mr.BroadcastBytes(user.NsConn, ClnRoomEvents.UserJoin, mr.g.name, []byte(user.Name))

	err := mr.SendBytes(user.NsConn, ClnRoomEvents.UserPrivateJoin, []byte(user.Name))
	if err != nil {
		panic(err)
	}
	//TODO: 將當時房間狀態送出給進入者 (想法: Game必須一併傳入當時桌面情況進來,因為room_manager只管發送與廣播)
}

// UserLeave 使用者離開房間
func (mr *RoomManager) UserLeave(user *RoomUser) {
	slog.Debug("UserLeave",
		slog.String("傳入資訊",
			fmt.Sprintf("姓名:%s  遊戲中:%t  區域:%s(%d)", user.Name, user.IsSitting, CbSeat(user.Zone8), user.Zone8)))

	//先判斷連線有否在遊戲中
	if user.NsConn.Conn.Get(KeyGame) != nil || user.IsSitting == true {
		mr.PlayerLeave(user)
	}

	//TBC 好像 Tracking只用來當成 switch的判斷,不需要使用 preTracking 這個機制
	// TODO 移除 preTracking
	preTracking := user.Tracking
	user.Tracking = LeaveRoom

	response := mr.door.Probe(user)

	if response.err != nil {
		//TODO 移除 Tracking還原
		user.Tracking = preTracking
		slog.Debug("使用者離開房間(UserLeave)", slog.String(".", response.err.Error()))
		if user.NsConn != nil && !user.NsConn.Conn.IsClosed() {
			user.NsConn.Emit(ClnRoomEvents.ErrorSpace, []byte(response.err.Error()))
		}
		user = nil
		return
	}

	//正常離開, 不正常離開的處理在 service.room.go - _OnRoomLeft
	mr.g.CounterSub(user.NsConn, mr.g.name)
	//告知client切換回大廳,後端只要移除Conn Store,前端會執行轉頁面到Lobby namespace
	user.NsConn.Conn.Set(KeyRoom, nil)
	user.NsConn.Conn.Set(KeyZone, nil)

	//TODO 廣播有人離開房間
	mr.BroadcastString(user.NsConn, ClnRoomEvents.UserLeave, mr.g.name, response.playerName)

	//不正常斷線, isClientBroken在KickOutBrokenConnection被設定
	if user.IsClientBroken {
		return
	}

	//正常斷線(離開房間,通知前端切換場景)
	err := mr.SendBytes(user.NsConn, ClnRoomEvents.UserPrivateLeave, []byte(user.Name))
	if err != nil {
		if errors.Is(err, ErrClientBrokenOrRefresh) {
			slog.Error("UserLeave", slog.String("發送通知訊息失敗", response.playerName), slog.String(".", err.Error()))
		}
		if errors.Is(err, ErrConn) {
			slog.Error("UserLeave", slog.String("發送通知訊息失敗", response.playerName), slog.String(".", err.Error()))
		}
	}
}

// playerJoin表示使用者要入桌入座,或離開座位
// 坐下: zoneSeat 表示坐定的位置 º 離座: zoneSeat 表示離座位置
func (mr *RoomManager) playerJoin(user *RoomUser, flag pb.SeatStatus) (zoneSeat uint8, userName string, isGameStart bool) {
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
				seatAt.player.TicketTime = atTime
				seatAt.player.Name = user.Name
				//seatAt.player.Zone8 = user.Zone8
				//seatAt.player.Zone = user.Zone

				zoneSeat = seatAt.zone // 入座
				user.Tracking = EnterGame
				mr.players++
				//回傳的zoneSeat不可能是 0x0
				return zoneSeat, seatAt.player.Name, mr.players >= 4
			}
		case pb.SeatStatus_StandUp:
			if seatAt.player.NsConn != nil && seatAt.player.NsConn == user.NsConn {
				slog.Debug("playerJoin", slog.String("StandUp 👍 ", fmt.Sprintf("座位:%s(%p) 連線:%p", CbSeat(seatAt.zone), seatAt.player.NsConn, user.NsConn)))

				//回傳離開座位者姓名
				userName = seatAt.player.Name
				zoneSeat = seatAt.zone // 離那個座

				seatAt.player.NsConn = nil // 離座
				seatAt.player.Play = uint32(valueNotSet)
				seatAt.player.Bid = uint32(valueNotSet)
				seatAt.player.Name = ""
				//seatAt.player.Zone = uint32(valueNotSet)
				//seatAt.player.Zone8 = valueNotSet

				user.Tracking = EnterRoom
				mr.players--
				return zoneSeat, userName, mr.players >= 4
			}
		}
	}
	slog.Debug("playerJoin", slog.String("FYI", fmt.Sprintf("(SitDown)=>遊戲座位已滿 | 或 (StandUp)玩家尚未入座(StandUp)◦ 目前桌中人數:%d", mr.players)))
	// 可能位置已滿,zoneSeat會是 valueNotSet,所以呼叫者可以判斷
	return zoneSeat, userName, mr.players >= 4
}

// PlayerJoin 加入, 底層透過呼叫 playerJoin, 最後判斷使否開局,與送出發牌
func (mr *RoomManager) PlayerJoin(user *RoomUser) {
	slog.Info("PlayerJoin", slog.String("傳入參數", fmt.Sprintf("%s %s(%d) %s", user.Name, CbSeat(user.Zone8), user.Zone8, shortConnID(user.NsConn))))

	user.Tracking = EnterGame

	var response chanResult

	//Probe內部用user name查詢是否user已經入房間
	response = mr.door.Probe(user)

	// 房間已滿(超出RoomUsersLimit), 或使用者已存在房間
	if response.err != nil {
		if user.NsConn != nil && !user.NsConn.Conn.IsClosed() {
			if errors.Is(response.err, ErrUserInPlay) {
				slog.Error("PlayerJoin", slog.String(".", fmt.Sprintf("%s 上座遊戲 %s座發生錯誤,因為使用者已在遊戲房間內", user.Name, CbSeat(user.Zone8))))
				user.NsConn.Emit(ClnRoomEvents.ErrorRoom, []byte("已在遊戲中"))
			}
			if errors.Is(response.err, ErrUserNotFound) {
				slog.Error("PlayerJoin", slog.String(".", fmt.Sprintf("%s 上座遊戲 %s座發生錯誤,因為使用者不在遊戲房間內", user.Name, CbSeat(user.Zone8))))
				user.NsConn.Emit(ClnRoomEvents.ErrorRoom, []byte("尚未進入遊戲房間"))
			}
		}
		return
	}

	// 房間已滿,已經晚一步
	if response.isGameStart && !response.isOnSeat {
		//Zorn
		//user.NsConn.Emit(ClnRoomEvents.ErrorRoom, []byte("座位已滿,已經晚一步"))
		return
	}

	user.NsConn.Conn.Set(KeyGame, response.seat) //表示玩家已進入遊戲中,設定遊戲中位置

	// 第一步: 上桌
	// 告訴玩家你已經上桌,前端必須處理, 往右移1位是因為舊的code是這樣寫的 TBC
	//user.NsConn.Emit(ClnRoomEvents.TablePrivateOnSeat, []byte{response.seat >> 1})
	//上座玩家
	//TODO: 連同桌中之前已經上座的玩家方位資訊一並丟回

	//step1 以 seat 從Ring找出NsConn
	request := &tableRequest{
		topic: _GetTablePlayers,
	}

	r := mr.table.Probe(request)

	pbPlayers := pb.PlayingUsers{}
	pbPlayers.Players = make([]*pb.PlayingUser, 0, 4)
	pbPlayers.Players = append([]*pb.PlayingUser{}, r.e.ToPbUser(), r.s.ToPbUser(), r.w.ToPbUser(), r.n.ToPbUser()) //RoomUser 轉 pbPlayer
	pbPlayers.ToPlayer = &pb.PlayingUser{
		Name:       user.Name,
		Zone:       uint32(response.seat),
		TicketTime: pb.LocalTimestamp(time.Now()),
		IsSitting:  response.isOnSeat,
	}

	//通知(private)個人玩家Player上座了
	payload := payloadData{
		ProtoData:   &pbPlayers,
		Player:      response.seat, /*必須指定送給誰*/
		PayloadType: ProtobufType,
	}
	mr.SendPayloadToPlayer(ClnRoomEvents.TablePrivateOnSeat, payload)

	//廣播(public)通知整區,ToPlayer玩家上座
	payload.ProtoData = pbPlayers.ToPlayer
	// Bug 底下SendPayLoadsToZone 可能會曝光.因為瀏覽器玩家多種登入的關係
	mr.SendPayloadsToZone(ClnRoomEvents.TableOnSeat, user.NsConn, payload)

	// 順利坐到位置剛好滿四人局開始
	slog.Debug("PlayerJoin", slog.Bool("isOnSeat", response.isOnSeat), slog.Bool("isGameStart", response.isGameStart))

	if response.isOnSeat && response.isGameStart {
		// g.start會洗牌,亂數取得開叫者,及禁叫品項, bidder首叫會是亂數取的
		mr.SendGameStart()
	}
}

func (mr *RoomManager) SendGameStart() {

	//通知(private)個人玩家Player上座了
	payload := payloadData{
		PayloadType: ProtobufType,
	}

	// 首引, 以及初始叫品(uint8(BidYet)) BidYet CbBid = iota = 0
	lead := mr.g.start()

	initZeroBid := uint8(BidYet)

	mr.g.SeatShift(lead)

	// 發牌
	mr.SendDeal()

	//延遲,是因為最後進來的玩家前端render速度太慢,會導致接收到NotyBid時來不及,所以延遲幾秒
	//time.Sleep(time.Millisecond * 700)

	// 注意 需要分別發送給桌面上的玩家通知 GamePrivateNotyBid
	//個人開叫提示, 前端 必須處理
	//TODO : 確認禁叫品就是當前最新的叫品,前端(label.dart-setBidTable)可以方便處理
	//BidOrder 第一次開叫時設定前端競叫版歷史紀錄表
	//lead 表示下一個開叫牌者 前端(Player,觀眾席)必須處理
	//禁叫品項,因為是首叫所以禁止叫品是 重要 zeroBid競叫開始
	//第三個參數:上一個叫牌者(ValueNotSet)
	//第四個參數: 上一次叫品(ValueNotSet)
	//第五個參數: 一線double value
	//第六個參數: 一線double 開啟 (0:表示disable)
	//第七個參數: 一線ReDouble value
	//第八個參數: 一線ReDouble 開啟 (0:表示disable)
	//   參考: GamePrivateNotyBid
	notyBid := cb.NotyBid{
		BidOrder: &cb.BidOrder{
			Headers: mr.g.GetBidOrder(),
		},
		Bidder:   uint32(lead),
		BidStart: uint32(initZeroBid),
		//LastBidder: uint32(valueNotSet),
		Double1: uint32(Db1),
		Double2: uint32(Db2),
		Btn:     cb.NotyBid_disable_all,
	}

	payload.ProtoData = &notyBid
	payload.PayloadType = ProtobufType

	// 重要
	// memo 底下通知順序很重要,一定要先Public,才Private, 因為Public前端會先初始 Bidding Table 設定
	//  step1 必須先送出Public (GameNotyBid),進行前端Bidding Table生成
	//  step2 才能再送出Private (GamePrivateNotyBid) 給當事人
	//mr.SendPayloadToPlayers(ClnRoomEvents.GameNotyBid, payload, pb.SceneType_game) //廣播Public
	mr.SendPayloadToPlayers(ClnRoomEvents.GameNotyBid, &notyBid, pb.SceneType_game) //廣播Public

	time.Sleep(time.Millisecond * 400)
	//指定傳送給 lead 開叫
	payload.Player = lead
	mr.SendPayloadToPlayer(ClnRoomEvents.GamePrivateNotyBid, payload) //私人Private

	return
}

// PlayerLeave 加入, 底層透過呼叫 playerJoin, 進行離桌程序
func (mr *RoomManager) PlayerLeave(user *RoomUser) {
	slog.Info("PlayerLeave", slog.String("傳入資訊", fmt.Sprintf("name:%s 入桌中:%t 欲離開%s(%d)", user.Name, user.IsSitting, CbSeat(user.Zone8), user.Zone8)))

	user.Tracking = LeaveGame

	var response chanResult

	response = mr.door.Probe(user)

	if response.err != nil {
		slog.Debug("PlayerLeave", slog.String(".", response.err.Error()))
		if user.NsConn != nil && !user.NsConn.Conn.IsClosed() {
			user.NsConn.Emit(ClnRoomEvents.ErrorRoom, []byte(response.err.Error()))
			return
		}
		return
	}

	// 玩家不在遊戲中
	if response.seat == valueNotSet {
		slog.Debug("PlayerLeave", slog.String(".", fmt.Sprintf("玩家%s不在遊戲中", shortConnID(user.NsConn))))
		return
	}

	//正常離開, 不正常離開處理在 service.room.go - _OnRoomLeft
	user.NsConn.Conn.Set(KeyGame, nil)
	user.NsConn.Conn.Set(KeyPlayRole, nil)

	//避免 KickOutBrokenConnection 中,執行UserLeave時再執行一次PlayerLeave
	user.IsSitting = false
	payload := payloadData{
		ProtoData: &pb.PlayingUser{
			Name:       response.playerName, //user.Name若為空表示玩家斷線,或browser refresh
			Zone:       uint32(response.seat),
			TicketTime: pb.LocalTimestamp(time.Now()),
		},
		Player:      response.seat,
		PayloadType: ProtobufType,
	}

	//TBC 因為Client可能不正常離線(離桌)所以可能已經失去連線,所以在此不需要再送訊號通知做私人通知
	//mr.SendBytes(user.NsConn, ClnRoomEvents.TablePrivateOnLeave, nil)
	//清除叫牌紀錄
	// moszorn 重要: 一並清除 bidHistories
	// 重要: TODO 3-13 moszorn 底下清除bidHistory 可能造成Data racing 參考: game.go - KickOutBrokenConnection 也有同樣的問題
	mr.g.engine.ClearBiddingState()

	//發送其它三位玩家清空桌面(因為有人離桌)
	//mr.SendPayloadToPlayers(ClnRoomEvents.TablePrivateOnLeave, payload, response.alives[:]) //response.alives[:]轉換array成為slice
	var signal byte = 0x7F //沒什麼,只代表發送給前端的訊號
	mr.SendByteToPlayers(ClnRoomEvents.TablePrivateOnLeave, signal, response.alives[:])

	//離開座位
	// 廣播已經有人離桌,前端必須處理(Disable上座功能),並顯示誰離座
	mr.SendPayloadsToZone(ClnRoomEvents.TableOnLeave, user.NsConn, payload)

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
		if tp.zone == seat && tp.player.NsConn != nil && !tp.player.NsConn.Conn.IsClosed() {
			found = true
			return tp, found
		}
		mr.Ring = mr.Next()
		tp = mr.Value.(*tablePlayer)
	}
	return nil, found
}

// FindPlayer 指定座位上的玩家(並非針對觀眾)
func (mr *RoomManager) FindPlayer(seat uint8) (nsConn *skf.NSConn, playerName string, isOnSeat, isGameStart bool, err error) {
	tps := &tableRequest{
		topic:  _FindPlayer,
		player: &RoomUser{Zone8: seat},
	}
	rep := mr.table.Probe(tps)
	if rep.err != nil {
		err = rep.err
		return
	}

	playerName = rep.playerName
	nsConn = rep.player
	isOnSeat = rep.isOnSeat
	isGameStart = rep.isGameStart

	if !isOnSeat {
		err = errors.New(fmt.Sprintf("找尋%s座位上的玩家%s不在座位上", CbSeat(seat), playerName))
		return
	}
	return
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
		switch CbSeat(zone) {
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
	// users 表示所有觀眾連線
	// 東南西北玩家(e,s,w,n player)分別是 ePlayer, sPlayer, wPlayer, nPlayer

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
		//排除已在座位上的玩家
		switch CbSeat(zone) {
		case east:
			player = ePlayer.NsConn
		case south:
			player = sPlayer.NsConn
		case west:
			player = wPlayer.NsConn
		case north:
			player = nPlayer.NsConn
		}
		// 限觀眾連線
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
		switch CbSeat(v.zone) {
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

// 指定排除某位玩家連線,撈出其它三家連線(用於通知遊戲中三位玩家有人離線,斷線)
func (mr *RoomManager) acquirePlayerConnectionsByExclude(exclude uint8) (c1, c2, c3 *skf.NSConn) {
	var c byte = 0
	mr.Do(func(i any) {
		v := i.(*tablePlayer)

		switch v.zone {
		case exclude:
			//do nothing
		default:
			switch c {
			case byte(0):
				c1 = v.player.NsConn
			case byte(1):
				c2 = v.player.NsConn
			case byte(2):
				c3 = v.player.NsConn
			}
			c++
		}
	})
	return
}

// AcquirePlayerConnections 從Ring中取得遊戲中四家連線
func (mr *RoomManager) AcquirePlayerConnections() (e, s, w, n *skf.NSConn) {
	//step1 以 seat 從Ring找出NsConn
	request := &tableRequest{
		topic: _GetTablePlayers,
	}

	response := mr.table.Probe(request)

	if response.err != nil {
		slog.Error("連取得線出錯(AcquirePlayerConnections)", slog.String(".", response.err.Error()))
		return
	}
	return response.e.NsConn, response.s.NsConn, response.w.NsConn, response.n.NsConn
}

// 回傳以第一個空位為始點的環形陣列,order 第一個元素就是空位的seat,用於使用者進入房間的位置方位
func (mr *RoomManager) lastLeaveOrder() (order [4]*RoomUser) {
	//Bug
	// bug 進入瀕繁,位置會亂跑

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
		// 坑: 這裡不懂
		var user *RoomUser = (seat.(*tablePlayer)).player
		user.Zone = uint32(user.Zone8)
		order[i] = user
		i++
	})
	return
}

// PlayersCardValue 撈取四位玩家打出的牌, 回傳的順序固定為 e(east), s(south), w(west), n(north)
func (mr *RoomManager) PlayersCardValue() (e, s, w, n uint8) {
	// TODO 是否需要 Lock 存取
	mr.Do(func(i any) {
		v := i.(*tablePlayer)
		switch CbSeat(v.zone) {
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
		slog.Debug("移動位置SeatShift", slog.String(".", response.err.Error()))
		return valueNotSet
	}
	//slog.Debug("移動位置SeatShift", slog.Bool("遊戲開始", response.isGameStart), slog.Int("回合動作", int(response.aa)))
	return response.seat
}

//SendXXXX 指資訊個別的送出給玩家,觀眾通常用於遊戲資訊
/* ============================================================================================
 BroadcastXXXX 用於廣播,無論玩家,觀眾都會同時送出同樣的訊息,通常用於公告,聊天資訊,遊戲共同訊息(叫牌)
======================== ====================================================================== */

// SendShowPlayersCardsOut 因為競叫流局,在重新發新牌前,四家攤牌(顯示另三家持牌)
func (mr *RoomManager) SendShowPlayersCardsOut() {
	tqs := &tableRequest{
		topic: _GetZoneUsers,
	}

	rep := mr.table.Probe(tqs)
	if rep.err != nil {
		slog.Error("發牌SendDeal錯誤", slog.String(".", rep.err.Error()))
	}
	//玩家發牌 - 順序是東,南,西,北家, 重要 所以前段順序也必須要配合
	//rep.e.NsConn, rep.s.NsConn, rep.w.NsConn, rep.n.NsConn
	actions := make(map[*skf.NSConn][]byte)
	actions[rep.e.NsConn] = nil
	actions[rep.s.NsConn] = nil
	actions[rep.w.NsConn] = nil
	actions[rep.n.NsConn] = nil

	eastHand, southHand, westHand, northHand := (*&mr.g.deckInPlay)[playerSeats[0]][:], (*&mr.g.deckInPlay)[playerSeats[1]][:], (*&mr.g.deckInPlay)[playerSeats[2]][:], (*&mr.g.deckInPlay)[playerSeats[3]][:]

	//東家: 發南,西,北
	//南家: 發西,北,東
	//西家: 發北,東,南
	//南家: 發西,北,東
	//用 cb.PlayersCards { uint32 seat, map<uint32, bytes> data}

	//發送給東家, 發南,西,北手持牌
	eastPlayer := &cb.PlayersCards{
		Seat: uint32(east),
		Data: map[uint32][]byte{
			uint32(south): southHand,
			uint32(west):  westHand,
			uint32(north): northHand}}
	//發送給南家: 發西,北,東手持牌
	southPlayer := &cb.PlayersCards{
		Seat: uint32(south),
		Data: map[uint32][]byte{
			uint32(west):  westHand,
			uint32(north): northHand,
			uint32(east):  eastHand}}
	//發送給西家: 發北,東,南手持牌
	westPlayer := &cb.PlayersCards{
		Seat: uint32(west),
		Data: map[uint32][]byte{
			uint32(north): northHand,
			uint32(east):  eastHand,
			uint32(south): southHand}}
	//發送給北家: 發東,南,西手持牌
	northPlayer := &cb.PlayersCards{
		Seat: uint32(north),
		Data: map[uint32][]byte{
			uint32(east):  eastHand,
			uint32(south): southHand,
			uint32(west):  westHand}}

	eastMarshal, err := pb.Marshal(eastPlayer)
	if err != nil {
		slog.Error("SendPlayersHandDeal(東)", slog.String(".", err.Error()))
	}
	actions[rep.e.NsConn] = eastMarshal

	southMarshal, err := pb.Marshal(southPlayer)
	if err != nil {
		slog.Error("SendPlayersHandDeal(南)", slog.String(".", err.Error()))
	}
	actions[rep.s.NsConn] = southMarshal

	westMarshal, err := pb.Marshal(westPlayer)
	if err != nil {
		slog.Error("SendPlayersHandDeal(西)", slog.String(".", err.Error()))
	}
	actions[rep.w.NsConn] = westMarshal

	northMarshal, err := pb.Marshal(northPlayer)
	if err != nil {
		slog.Error("SendPlayerHandDeal(北)", slog.String(".", err.Error()))
	}
	actions[rep.n.NsConn] = northMarshal

	var (
		payload    []byte
		connection *skf.NSConn
		eventName  string = ClnRoomEvents.GameCardsShowUp
	)
	for connection, payload = range actions {
		connection.EmitBinary(eventName, payload)
	}

}

// SendDealToPlayer 向入座遊戲中的玩家發牌,與SendDealToZone不同, SendDealToPlayer向指定玩家發牌
func (mr *RoomManager) sendDealToPlayer( /*deckInPlay *map[uint8]*[NumOfCardsOnePlayer]uint8, */ connections ...*skf.NSConn) {
	// playersHand 以Seat為Key,Value代表該Seat的待發牌
	// deckInPlay 由 Game傳入
	// 注意: connections 與 deckInPlay順序必須一致 (ease, south, west, north)
	var player *skf.NSConn
	for idx := range connections {
		player = connections[idx]
		if player != nil && !player.Conn.IsClosed() {
			player.EmitBinary(
				ClnRoomEvents.GamePrivateDeal,
				(*&mr.g.deckInPlay)[playerSeats[idx]][:],
				/*(*deckInPlay)[playerSeats[idx]][:] */)
		} else {
			//TODO 其中有一個玩家斷線,就停止遊戲,並通知所有玩家, Player
			slog.Error("連線(SendDeal)中斷", slog.String(".", fmt.Sprintf("%s發牌連線中斷", CbSeat(playerSeats[idx]))))
		}
	}
}

// SendDealToZone 向 Zone發牌, 但是必須濾除掉在該Zone的 Player, 因為 Player是透過 SendDealToPlayer發牌
func (mr *RoomManager) sendDealToZone( /*deckInPlay *map[uint8]*[NumOfCardsOnePlayer]uint8, */ users []*skf.NSConn) {
	//eHand, sHand, wHand, nHand := (*deckInPlay)[playerSeats[0]][:], (*deckInPlay)[playerSeats[1]][:], (*deckInPlay)[playerSeats[2]][:], (*deckInPlay)[playerSeats[3]][:]

	// 4個座位player手持牌
	/*
		eHand, sHand, wHand, nHand :=
			(*&mr.g.deckInPlay)[playerSeats[0]][:],
			(*&mr.g.deckInPlay)[playerSeats[1]][:],
			(*&mr.g.deckInPlay)[playerSeats[2]][:],
			(*&mr.g.deckInPlay)[playerSeats[3]][:]

		for i := range users {
			users[i].EmitBinary(ClnRoomEvents.GameDeal, eHand)
			users[i].EmitBinary(ClnRoomEvents.GameDeal, sHand)
			users[i].EmitBinary(ClnRoomEvents.GameDeal, wHand)
			users[i].EmitBinary(ClnRoomEvents.GameDeal, nHand)
		}*/
	eHand, sHand, wHand, nHand :=
		(*&mr.g.deckInPlay)[playerSeats[0]][:],
		(*&mr.g.deckInPlay)[playerSeats[1]][:],
		(*&mr.g.deckInPlay)[playerSeats[2]][:],
		(*&mr.g.deckInPlay)[playerSeats[3]][:]

	cards := make([]byte, 0, 55)
	cards = append(cards, eHand...)
	cards = append(cards, _cover)
	cards = append(cards, sHand...)
	cards = append(cards, _cover)
	cards = append(cards, wHand...)
	cards = append(cards, _cover)
	cards = append(cards, nHand...)
	//slog.Debug("sendDealToZone-55張", slog.Int("張數", len(cards)))
	//slog.Debug("sendDealToZone-牌", slog.Any("東", eHand))
	//slog.Debug("sendDealToZone-牌", slog.Any("南", sHand))
	//slog.Debug("sendDealToZone-牌", slog.Any("西", wHand))
	//slog.Debug("sendDealToZone-牌", slog.Any("北", nHand))

	slog.Debug("sendDealToZone-觀眾發牌", slog.Int("觀眾數", len(users)))
	//向觀眾送出四位玩家的牌
	for i := range users {
		users[i].EmitBinary(ClnRoomEvents.GameDeal, cards)
	}
}

// SendDeal 向玩家, 觀眾(Player)發牌, 送出 bytes
func (mr *RoomManager) SendDeal( /*deckInPlay *map[uint8]*[NumOfCardsOnePlayer]uint8*/ ) {
	tqs := &tableRequest{
		topic: _GetZoneUsers,
	}

	rep := mr.table.Probe(tqs)
	if rep.err != nil {
		slog.Error("發牌SendDeal錯誤", slog.String(".", rep.err.Error()))
	}

	// *map[uint8]*[NumOfCardsOnePlayer]uint8
	//deckInPlay := &mr.g.deckInPlay

	//玩家發牌 - 順序是東,南,西,北家, 重要 所以前段順序也必須要配合
	mr.sendDealToPlayer(rep.e.NsConn, rep.s.NsConn, rep.w.NsConn, rep.n.NsConn)

	/*	for i := range rep.audiences {
			roomUser := rep.audiences[i]
			fmt.Printf("(%s)觀眾%d %s isSitting:%t\n", CbSeat(roomUser.Zone8), i, roomUser.Name, roomUser.IsSitting)
		}
	*/
	//觀眾發牌
	rep.audiences.DumpNames("SendDeal-目前觀眾") //列出哪些是觀眾
	mr.sendDealToZone(rep.audiences.Connections())
}

// send 針對payload型態對連線發送 []byte 或 proto bytes
func (mr *RoomManager) send(nsConn *skf.NSConn, eventName string, payload payloadData) error {

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

// SendBytes 對個別連線送出byte,或 bytes
func (mr *RoomManager) SendBytes(nsConn *skf.NSConn, eventName string, bytes []uint8) error {

	if nsConn == nil || nsConn.Conn.IsClosed() {
		//不正常斷線,或Client Refresh會發生
		return ErrClientBrokenOrRefresh
	}
	ok := nsConn.EmitBinary(eventName, bytes)
	if !ok {
		return ErrConn
	}
	return nil
}

func (mr *RoomManager) sendBytesToPlayers(payload []byte, eventName string) {

	var connections [4]*skf.NSConn
	connections[0], connections[1], connections[2], connections[3] = mr.AcquirePlayerConnections()

	var player *skf.NSConn
	for idx := range connections {
		player = connections[idx]
		if player != nil && !player.Conn.IsClosed() {
			player.EmitBinary(eventName, payload)
		} else {
			//TODO 其中有一個玩家斷線,就停止遊戲,並通知所有玩家, Player
			slog.Error("連線(sendBytesToPlayers)中斷", slog.String(".", fmt.Sprintf("%ssendBytesToPlayers連線中斷", CbSeat(playerSeats[idx]))))
		}
	}
}

// SendByteToPlayers 發送byte訊息,TODO: 需要被 Refactor. 不應該傳入 []*skf.NSConn
func (mr *RoomManager) SendByteToPlayers(eventName string, payload byte, connections []*skf.NSConn) {
	for i := range connections {
		mr.SendBytes(connections[i], eventName, []byte{payload})
	}
}

/*
func (mr *RoomManager) SendPayloadToPlayers(eventName string, payload payloadData, connections []*skf.NSConn) {
	// connections 可以是一個玩家,兩個玩家,三個玩家,四個玩家
	var player *skf.NSConn
	for idx := range connections {
		player = connections[idx]
		if player != nil && !player.Conn.IsClosed() {
			mr.send(player, eventName, payload)
		} else {
			//TODO 其中有一個玩家斷線,就停止遊戲,並通知所有玩家, Player
			slog.Error("連線(sendToPlayers)中斷", utilog.Err(fmt.Errorf("發送事件%s", eventName)))
		}
	}
}*/

// SendPayloadToPlayers 對遊戲中四位玩家發一則訊息,
// sceneType表示正於何種場景進行訊息發送,若有人斷線可作相應處理,例如:遊戲場景,送出清除遊戲場警訊號
func (mr *RoomManager) SendPayloadToPlayers(eventName string, protoMessage proto.Message, sceneType pb.SceneType) error {
	var (
		err          error
		errFmtString = "%s 玩家連線中斷"
		connections  = make(map[uint8]*skf.NSConn)
		e, s, w, n   = uint8(east), uint8(south), uint8(west), uint8(north)
		seat         uint8

		payload = payloadData{
			ProtoData:   protoMessage,
			PayloadType: ProtobufType,
		}
	)

	connections[e], connections[s], connections[w], connections[n] = mr.AcquirePlayerConnections()

	if connections[e] == nil {
		err = fmt.Errorf(errFmtString, east)
		seat = e
	}
	if connections[s] == nil {
		err = fmt.Errorf(errFmtString, south)
		seat = s
	}
	if connections[w] == nil {
		err = fmt.Errorf(errFmtString, west)
		seat = w
	}
	if connections[n] == nil {
		err = fmt.Errorf(errFmtString, north)
		seat = n
	}

	if err != nil {
		slog.Error("連線中斷(SendPayloadToPlayers)", slog.String(".", err.Error()))
		//TODO 對未斷線玩家,送出現在狀況,好讓前端popup
		for _, nsConn := range connections {
			// TODO 通知前端清除畫面,並告知有人斷線
			if nsConn != nil {
				payload.PayloadType = ProtobufType
				payload.ProtoData = &pb.ErrMessage{
					Msg:   err.Error(),
					Alert: false,
					Seat:  uint32(seat),
					Scene: sceneType, /*表示哪種訊息*/
				}
				if errAgain := mr.send(nsConn, ClnRoomEvents.GameAlertMessage, payload); errAgain != nil {
					// 吞掉再一次錯誤,因為上面的錯誤會通知已經通知前端處理了
					slog.Error("SendPayloadToPlayers", slog.String(".", errAgain.Error()))
				}
			}
		}
		//回傳錯誤好讓呼叫者清空遊戲狀態
		return err

	} else {
		var player *skf.NSConn
		for idx := range connections {
			player = connections[idx]
			if player != nil && !player.Conn.IsClosed() {
				mr.send(player, eventName, payload)
			} else {
				//TODO 其中有一個玩家斷線,就停止遊戲,並通知所有玩家, Player
				slog.Error("連線(sendToPlayers)中斷", slog.String(",", fmt.Sprintf("發送事件%s", eventName)))
			}
		}
	}
	return nil
}

//--------------------------------------------------------------------

// SendPayloadToTwoPlayer 將同一個封包送向進攻方(莊,夢)
func (mr *RoomManager) SendPayloadToTwoPlayer(eventName string, protoMessage proto.Message, player1, player2 uint8) error {
	var (
		err error
		//三家connection
		connections = make(map[uint8]*skf.NSConn)
		e, s, w, n  = uint8(east), uint8(south), uint8(west), uint8(north)
		payload     = payloadData{
			ProtoData:   protoMessage,
			PayloadType: ProtobufType,
		}
	)

	connections[e], connections[s], connections[w], connections[n] = mr.AcquirePlayerConnections()
	for seat, con := range connections {
		switch seat {
		case player1, player2:
			// 莊夢兩家送出同樣的封包
			err = mr.send(con, eventName, payload)
		default:
			continue
		}
	}
	return err
}

// SendPayloadTo3PlayersByExclude 使用exclude(排除掉玩家),對桌上三個玩家送出同一事件的封包
// 場景: 每一回合和開始時,有三家收到PlayNotice對gauge進行不帶回呼的計數, 而實際下一位玩家則另外專門封包通知,已開啟帶回呼的計數(gauge)
func (mr *RoomManager) SendPayloadTo3PlayersByExclude(eventName string, protoMessage proto.Message, exclude uint8) {
	var (
		err          error
		errFmtString = "%s玩家連線中斷"
		//三家connection
		connections = make(map[uint8]*skf.NSConn)
		e, s, w, n  = uint8(east), uint8(south), uint8(west), uint8(north)

		payload = payloadData{
			ProtoData:   protoMessage,
			PayloadType: ProtobufType,
		}
	)

	connections[e], connections[s], connections[w], connections[n] = mr.AcquirePlayerConnections()

	if connections[e] == nil {
		err = fmt.Errorf(errFmtString, "east")
	}
	if connections[s] == nil {
		err = fmt.Errorf(errFmtString, "north")
	}
	if connections[w] == nil {
		err = fmt.Errorf(errFmtString, "west")
	}
	if connections[n] == nil {
		err = fmt.Errorf(errFmtString, "north")
	}

	if err != nil {
		slog.Error("連線中斷(SendPayloadTo3PlayersByExclude)", slog.String(".", err.Error()))
		//TODO 對未斷線玩家,送出現在狀況,好讓前端popup
		for _, nsConn := range connections {
			if nsConn != nil {
				nsConn.EmitBinary("popup-warning", []byte(err.Error()))
			}
		}

	} else {
		for seat, con := range connections {
			if con != nil {
				switch seat {
				case exclude:
					//skip exclude 玩家封包發送
				default:
					// 三家送出同樣的封包
					err = mr.send(con, eventName, payload)
				}
			} else {
				//TODO: 斷線處理
				slog.Warn("payload發送失敗(SendPayloadTo3PlayersByExclude)", slog.String(".", fmt.Sprintf("座位%s %s", CbSeat(seat), err)))
			}
		}

		if err != nil {
			slog.Warn("payload發送失敗(SendPayloadTo3PlayersByExclude)", slog.String(".", err.Error()))
		}
	}

}

// SendPayloadToOneAndPayloadToOthers
// 對同樣的事件EventName, 送出 commonPayload  給三個指定玩家,而送出 specialPayload 給指定玩家(specialSeat)
// 例如: 對西,北,南送出蓋牌出牌, 但東要送出明牌出牌 (用於玩家出牌)
func (mr *RoomManager) SendPayloadToOneAndPayloadToOthers(
	eventName string, commonPayload, specialPayload payloadData, specialSeat uint8) {

	var (
		err          error
		errFmtString = "%s玩家連線中斷"
		//三家connection
		connections = make(map[uint8]*skf.NSConn)
		e, s, w, n  = uint8(east), uint8(south), uint8(west), uint8(north)
	)

	connections[e], connections[s], connections[w], connections[n] = mr.AcquirePlayerConnections()

	if connections[e] == nil {
		err = fmt.Errorf(errFmtString, "east")
	}
	if connections[s] == nil {
		err = fmt.Errorf(errFmtString, "north")
	}
	if connections[w] == nil {
		err = fmt.Errorf(errFmtString, "west")
	}
	if connections[n] == nil {
		err = fmt.Errorf(errFmtString, "north")
	}

	if err != nil {
		slog.Error("連線中斷(SendPayloadToOneAndPayloadToOthers)", slog.String(".", err.Error()))
		//TODO 對未斷線玩家,送出現在狀況,好讓前端popup
		for _, nsConn := range connections {
			if nsConn != nil {
				nsConn.EmitBinary("popup-warning", []byte(err.Error()))
			}
		}

	} else {
		for seat, con := range connections {
			if con != nil {
				switch seat {
				case specialSeat:
					//specialPayload specialSeat 送出專門針對他(specialSeat)的封包
					err = mr.send(con, eventName, specialPayload)
				default:
					//commonPayload 三家送出同樣的封包
					err = mr.send(con, eventName, commonPayload)
				}
			} else {
				//TODO: 斷線處理
				slog.Warn("payload發送失敗(SendPayloadToOneAndPayloadToOthers)", slog.String(".", fmt.Sprintf("座位%s %s", CbSeat(seat), err)))
			}
		}

		if err != nil {
			slog.Warn("payload發送失敗(SendPayloadToOneAndPayloadToOthers)", slog.String(".", err.Error()))
		}
	}
}

// SendPayloadToDefendersToAttacker 分別送給莊夢一包, 防家們一包
// 莊(declarer),夢(dummy)發送 attackerPayload, 防家們(defenders)發送 defenderPayload
func (mr *RoomManager) SendPayloadToDefendersToAttacker(eventName string,
	defender2, attacker2 proto.Message,
	declarer, dummy uint8) {

	var (
		err          error
		errFmtString = "%s玩家連線中斷"
		//三家connection
		connections = make(map[uint8]*skf.NSConn)
		e, s, w, n  = uint8(east), uint8(south), uint8(west), uint8(north)

		defenderPayload, attackerPayload payloadData = payloadData{
			ProtoData:   defender2,
			PayloadType: ProtobufType,
		},
			payloadData{
				ProtoData:   attacker2,
				PayloadType: ProtobufType,
			}
	)

	connections[e], connections[s], connections[w], connections[n] = mr.AcquirePlayerConnections()

	if connections[e] == nil {
		err = fmt.Errorf(errFmtString, "east")
	}
	if connections[s] == nil {
		err = fmt.Errorf(errFmtString, "north")
	}
	if connections[w] == nil {
		err = fmt.Errorf(errFmtString, "west")
	}
	if connections[n] == nil {
		err = fmt.Errorf(errFmtString, "north")
	}

	if err != nil {
		slog.Error("連線中斷(SendPayloadToDefendersToAttacker)", slog.String(".", err.Error()))
		//TODO 對未斷線玩家,送出現在狀況,好讓前端popup
		for _, nsConn := range connections {
			if nsConn != nil {
				nsConn.EmitBinary("popup-warning", []byte(err.Error()))
			}
		}

	} else {
		for seat, con := range connections {
			if con != nil {
				switch seat {
				case declarer:
					fallthrough /*莊,夢*/
				case dummy:
					err = mr.send(con, eventName, attackerPayload)
				default: /*防家*/
					err = mr.send(con, eventName, defenderPayload)
				}
			} else {
				//TODO: 斷線處理
				slog.Warn("payload發送失敗(SendPayloadToDefendersToAttacker)", slog.String(".", fmt.Sprintf("座位%s %s", CbSeat(seat), err)))
			}
		}

		if err != nil {
			slog.Warn("payload發送失敗(SendPayloadToDefendersToAttacker)", slog.String(".", err.Error()))
		}
	}
}

//--------------------------------------------------------------------

// SendPayloadToPlayer 發送訊息給payload中指定的player, 指定eventName, 訊息 payload (payload內部必須指定seat(player))
func (mr *RoomManager) SendPayloadToPlayer(eventName string, payload payloadData /*, seat uint8*/) error {
	//slog.Debug("SendPayloadsToPlayer", slog.String("發送", fmt.Sprintf("%s(%d)", CbSeat(payload.Player), payload.Player)))

	//重要: payload 必取指定 Player (seat)
	conn, name, found, _, err := mr.FindPlayer(payload.Player)

	if err != nil {
		return err
	}

	if !found {
		slog.Error("SendPayloadsToPlayer", slog.String(".", fmt.Sprintf("未找到%s可進行發送", name)))
		return nil
	}
	err = mr.send(conn, eventName, payload)
	if err != nil {
		return err
	}
	return nil
}

// SendDummyCardsByExcludeDummy 夢家向三家現牌, eventName事件名稱, dummyHand夢家持牌, dummySeat 夢家, err 送封包錯誤
// 這個方法不會向夢家發送封包,但會向另外三家發送
func (mr *RoomManager) SendDummyCardsByExcludeDummy(eventName string, dummyHand *[]uint8, dummySeat uint8) (err error) {
	// 排除 dummy 不送
	var (
		connections = make(map[uint8]*skf.NSConn)
		dummyCards  = &cb.PlayersCards{
			Seat: uint32(dummySeat),
			Data: make(map[uint32][]uint8),
		}
		payload = payloadData{
			PayloadType: ProtobufType,
			ProtoData:   dummyCards,
		}
		lastSend uint32
	)

	connections[uint8(east)], connections[uint8(south)], connections[uint8(west)], connections[uint8(north)] = mr.AcquirePlayerConnections()

	for seat, conn := range connections {
		switch seat {
		case dummySeat:
			continue
		default:
			// key 改變,但map value只會allocate一次
			switch len(dummyCards.Data) {
			case 0:
				lastSend = uint32(seat)
				// allocate only once at first key
				dummyCards.Data[lastSend] = *dummyHand
			case 1:
				delete(dummyCards.Data, lastSend)
				lastSend = uint32(seat)
				dummyCards.Data[lastSend] = *dummyHand
			}
			if conn == nil || conn.Conn.IsClosed() {
				//DO log
				slog.Warn("SendDummyCardsByExcludeDummy", slog.String(".", fmt.Sprintf("%s斷線,或離開", CbSeat(seat))))
				continue
			}
			if err = mr.send(conn, eventName, payload); err != nil {
				//DO log
				slog.Error("SendDummyCardsByExcludeDummy", slog.String(".", err.Error()))
			}
		}
	}
	return
}

// SendPayloadsToPlayers 同時對遊戲中玩家發送訊息(payload)
// 坑) 注意: 每個payload都要指定Player(表示要發給的對象), 重要 否則發不出去
//
//	Example: 假如夢家的牌要亮給另三家,則payloads中會有這三家(哪三家則是透過 payload.Player指定
/*
// memo 已廢棄: 向三家亮出夢家牌 (
	g.roomManager.SendPayloadsToPlayers(ClnRoomEvents.GamePrivateShowHandToSeat,
		payloadData{
			ProtoData: &cb.PlayersCards{
				  Seat: uint32(g.Dummy), //亮夢家牌
				  Data: map[uint32][]uint8{
				  uint32(g.Defender): g.deckInPlay[uint8(g.Dummy)][:], //向防家亮夢家
				 },
				},
				 PayloadType: ProtobufType,
				 Player:      uint8(g.Defender), //坑:不要忘了加上Player,否則直接送往東(zero value)
				},
				payloadData{
				  ProtoData: &cb.PlayersCards{
				  Seat: uint32(g.Dummy), //亮夢家牌
				  Data: map[uint32][]uint8{
				  uint32(g.Lead): g.deckInPlay[uint8(g.Dummy)][:], //向首引(防家)亮夢家
				 },
				},
				 PayloadType: ProtobufType,
				 Player:      uint8(g.Lead) //payload send target,
				},
				payloadData{
				  ProtoData: &cb.PlayersCards{
				  Seat: uint32(g.Dummy), //亮夢家牌
				  Data: map[uint32][]uint8{
				  uint32(g.Declarer): g.deckInPlay[uint8(g.Dummy)][:], //向莊家亮夢家
				 },
				},
				 PayloadType: ProtobufType,
				 Player:      uint8(g.Declarer),
		 })
*/
func (mr *RoomManager) SendPayloadsToPlayers(eventName string, payloads ...payloadData) {

	var (
		err          error
		errFmtString = "%s玩家連線中斷"
		connections  = make(map[uint8]*skf.NSConn)
		e, s, w, n   = uint8(east), uint8(south), uint8(west), uint8(north)
	)

	connections[e], connections[s], connections[w], connections[n] = mr.AcquirePlayerConnections()

	if connections[e] == nil {
		err = fmt.Errorf(errFmtString, "east")
	}
	if connections[s] == nil {
		err = fmt.Errorf(errFmtString, "north")
	}
	if connections[w] == nil {
		err = fmt.Errorf(errFmtString, "west")
	}
	if connections[n] == nil {
		err = fmt.Errorf(errFmtString, "north")
	}

	if err != nil {
		slog.Error("連線中斷(SendPayloadsToPlayers)", slog.String(".", err.Error()))
		//TODO 對未斷線玩家,送出現在狀況,好讓前端popup
		for _, nsConn := range connections {
			if nsConn != nil {
				nsConn.EmitBinary("popup-warning", []byte(err.Error()))
			}
		}

	} else {
		for i := range payloads {
			err = mr.send(connections[payloads[i].Player], eventName, payloads[i])
			if err != nil {
				slog.Error("payload發送失敗(SendPayloadsToPlayers)", slog.String(",", err.Error()))
				continue
			}
		}
	}
}

// SendPayloadsToZone 針對所有的觀眾(但不包含玩家exclude,但含另三家玩家)發送訊息, exclude 排除連線
func (mr *RoomManager) SendPayloadsToZone(eventName string, exclude *skf.NSConn, payloads ...payloadData) {
	slog.Debug("SendPayloadsToZone", slog.String("發送", fmt.Sprintf("接收人數:%d , 排除發送者:%t", len(payloads), exclude != nil)))
	tqs := &tableRequest{
		topic: _GetZoneUsers,
	}
	rep := mr.table.Probe(tqs)
	if rep.err != nil {
		slog.Error("發送訊息錯誤(SendPayloadsToZone)", slog.String(".", rep.err.Error()))
	}

	var err error

	//濾掉玩家, 底下一定會有一個if是不成立
	include := make([]*skf.NSConn, 0, 3)
	if rep.e.NsConn != exclude && rep.e.NsConn != nil {
		include = append(include, rep.e.NsConn)
	}
	if rep.s.NsConn != exclude && rep.s.NsConn != nil {
		include = append(include, rep.s.NsConn)
	}
	if rep.w.NsConn != exclude && rep.w.NsConn != nil {
		include = append(include, rep.w.NsConn)
	}
	if rep.n.NsConn != exclude && rep.n.NsConn != nil {
		include = append(include, rep.n.NsConn)
	}

	connections := rep.audiences.Connections()

	// 0表示0個玩家, <4 表示排除自己另三個玩家
	slog.Debug("SendPayloadsToZone", slog.Int("發送玩家", len(include)), slog.Int("發送觀眾", len(connections)))

	if len(include) > 0 && len(include) < 4 {
		//將自己以外的三位玩家也加入到廣播觀眾群
		for i := range include {
			connections = append(connections, include[i])
		}

	} else {
		if len(include) > 0 {
			slog.Error("發送廣播SendPayloadsToZone", slog.String(".", fmt.Sprintf("放送座位上玩家數量%d有問題", len(include))))
			return
		}
	}

	for i := range connections {
		for j := range payloads {
			if err = mr.send(connections[i], eventName, payloads[j]); err != nil {
				slog.Error("payload發送失敗(SendPayloadsToZone)", slog.String(".", err.Error()))
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
			if b.sender == Ns && isSkip {
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
func broadcastMsg(eventName, roomName string, serializedBody []uint8, errInfo error) (msg *skf.Message) {
	//sender sender不為nil情況下只會發生在傳送聊天訊息時,通常sender會是nil
	// roomName送到那個Room (TBC 要與前端確認)
	// serializedBody 發送的封包
	// errInfo 發送給前端必須處理的錯誤訊息

	msg = new(skf.Message)
	msg.Namespace = RoomSpaceName
	msg.Room = roomName
	msg.Event = eventName
	msg.Body = serializedBody
	msg.SetBinary = true
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
		msg:    broadcastMsg(eventName, roomName, serializedBody, errInfo),
		sender: sender,
		to:     nil,
		chat:   true,
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastChat")
}

// BroadcastBytes 發送 []uint8 封包給所有人, sender 排除廣播發送者, eventName Client事件, roomName房間名, serializedBody封包
func (mr *RoomManager) BroadcastBytes(sender *skf.NSConn, eventName, roomName string, serializedBody []uint8) {
	b := &broadcastRequest{
		msg:    broadcastMsg(eventName, roomName, serializedBody, nil),
		sender: sender,
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastBytes")
}

// BroadcastByte 發送 uint8 給所有人, sender 排除廣播發送者, eventName事件名稱, roomName廣播至哪裡, body廣播資料
func (mr *RoomManager) BroadcastByte(sender *skf.NSConn, eventName, roomName string, body uint8) {
	b := &broadcastRequest{
		msg:    broadcastMsg(eventName, roomName, []byte{body}, nil),
		sender: sender,
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastByte")
}

// BroadcastString 發送字串內容給所有人, sender 排除廣播發送者, eventName事件名稱, roomName廣播至哪裡, body廣播資料
func (mr *RoomManager) BroadcastString(sender *skf.NSConn, eventName, roomName string, body string) {
	b := &broadcastRequest{
		msg:    broadcastMsg(eventName, roomName, []byte(body), nil),
		sender: sender,
	}
	checkBroadcastError(mr.broadcastMsg.Probe(b), "BroadcastString")
}

// BroadcastProtobuf 發送protobuf 給所有人, sender 排除廣播發送者, eventName事件名稱, roomName廣播至哪裡, body廣播資料
func (mr *RoomManager) BroadcastProtobuf(sender *skf.NSConn, eventName, roomName string, body proto.Message) {

	marshal, err := pb.Marshal(body)
	if err != nil {
		slog.Error("ProtoMarshal(BroadcastProtobuf)", slog.String(".", err.Error()))
		return
	}

	mr.BroadcastBytes(sender, eventName, roomName, marshal)
}

// DevelopBroadcastTest user用於測試 BroadcastChat
func (mr *RoomManager) DevelopBroadcastTest(user *RoomUser) {
	roomName := "room0x0" //room0x0 room0x1
	eventName := ClnRoomEvents.DevelopBroadcastTest

	//byte
	//廣播byte  👍
	payloads := []uint8{uint8(north)}
	mr.BroadcastBytes(nil, eventName, roomName, payloads)
	time.Sleep(time.Second * 2)

	//bytes (前端bytes與 protobuf 互斥)
	//廣播bytes  👍
	//payloads = append(payloads, south, west, east)
	//mr.BroadcastBytes(eventName, roomName, payloads)

	//string
	//廣播字串  👍
	//mr.BroadcastBytes(eventName, roomName, []byte("日本字 人間にんげん"))

	//protobuf  廣播使用 protobuf,就不能再使用 string, values 因為是前端限制
	//廣播  👍 Protobuf
	/*
		message := pb.MessagePacket{
			Type:    pb.MessagePacket_Admin,
			Content: "hello MessagePacket",
			Tt:      pb.LocalTimestamp(time.Now()),
			RoomId:  12,
			From:    "蔡忠正",
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
		mr.BroadcastProtobuf(nil, eventName, roomName, &packet)
	*/
}

func (mr *RoomManager) DevelopPrivatePayloadTest(user *RoomUser) {
	fmt.Println("[DevelopPrivatePayloadTest]")
	eventName := ClnRoomEvents.DevelopPrivatePayloadTest

	p := payloadData{}
	//case1 byte ,前端判斷 msg.value 只要不為null, 就可取出byte值
	p.PayloadType = ByteType
	p.Data = []byte{uint8(east)}
	p.Player = uint8(east)
	p.ProtoData = nil
	mr.send(user.NsConn, eventName, p) // 👍

	//case2 bytes ,前端判斷 msg.values 只要不為null, 就可取出bytes值
	//(前端bytes與 protobuf 互斥)
	/*	p.PayloadType = ByteType
		p.PayloadType = ByteType
		p.Data = append(p.Data, south, west, north)
		p.Player = east
		p.ProtoData = nil
		mr.send(user.NsConn, p, eventName)
	*/

	//case3  👍 proto ,前端判斷 msg.pbody只要不為null, 就可取出pbody(protobuf)值
	/*
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
		mr.send(user.NsConn, eventName, p) // 👍

		//case4 String ,前端判斷 msg.body只要不為null, 就可取出string值
		p.PayloadType = ByteType
		p.Data = p.Data[:]
		p.Data = []uint8("人間にんげん")
		mr.send(user.NsConn, eventName, p) // 👍
	*/
}

// 檢驗BroadcastXXXX後的結果,並log錯誤
func checkBroadcastError(probe AppErr, broadcastName string) {
	if probe.Code != AppCodeZero {
		errorSubject := fmt.Sprintf("訊息送出失敗(%s)", broadcastName)
		switch probe.Code {
		case BroadcastC | NSConnC:
			slog.Error("嚴重錯誤(BroadcastChat)", slog.String(".", probe.Err.Error()))
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
