package project

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/moszorn/pb"
	"github.com/moszorn/pb/cb"
	utilog "github.com/moszorn/utils/log"
	"github.com/moszorn/utils/skf"
	"project/game"
)

func NewRoomSpaceService(rooms *map[string]*game.Game) AllRoom {
	if len(*rooms) == 0 {
		panic("key不存在")
	}

	var roomIdSeq int32 = 1
	for roomName := range *rooms {
		(*rooms)[roomName] = game.CreateCBGame(roomName, roomIdSeq)
		roomIdSeq++
	}
	return *rooms
}

// AllRoom Key: 房間名稱/Id , Value: 房間服務
// AllRoom 也應該實作 RoomService
type AllRoom map[string]*game.Game // interface should be Game

func (rooms AllRoom) userLeaveRoom(ns *skf.NSConn, m skf.Message) error {
	roomLog(ns, m)
	var (
		srv = rooms[m.Room]
		res = srv.UserLeaveChannel(ns)
	)
	if res.Err != nil {
		ns.Emit(ClnRoomEvents.ErrorRoom, []byte(res.Err.Error()))
		return res.Err
	}

	msg := []byte(fmt.Sprintf("%s已離開%s房間", ns, m.Room))
	//TODO
	srv.BroadcastString(ns, ClnRoomEvents.UserLeaveRoom, msg)

	return nil
}

func (rooms AllRoom) playerOnLeave(ns *skf.NSConn, m skf.Message) error {
	var (
		srv = rooms[m.Room]
	)
	res := srv.PlayerLeaveChannel(ns)
	if res.Err != nil {
		ns.Emit(ClnRoomEvents.ErrorRoom, []byte(res.Err.Error()))
		return res.Err
	}
	ns.Emit(ClnRoomEvents.Private, []byte("你已經離桌"))

	//房間廣播
	msg := []byte(fmt.Sprintf("%s離開%s遊戲", ns, m.Room))
	ns.Conn.Server().Broadcast(ns, skf.Message{
		Namespace: m.Namespace,
		Room:      m.Room,
		Event:     ClnRoomEvents.PlayerOnLeave,
		Body:      msg,
	})

	srv.BroadcastString(ns, ClnRoomEvents.PlayerOnLeave, msg)
	return nil
}
func (rooms AllRoom) playerOnSeat(ns *skf.NSConn, m skf.Message) error {
	var (
		srv = rooms[m.Room]
	)
	roomLog(ns, m)
	status, seatAt, bidder, forbidden, err := srv.PlayerJoinChannel(ns)
	if err != nil {
		switch status {
		case game.SeatFullBecauseGameStart:
		case game.SeatGameNA:
			// 未來 這裡發生不照預期的錯誤,所以必須記db log
			//game.ErrUserNotFound
			//game.ErrPlayMultipleGame
			//game.ErrUserInPlay
		}
		ns.Emit(ClnRoomEvents.ErrorRoom, []byte(err.Error()))
		return err
	}
	//入座者發送通知 (seat uint8Seat)
	slog.Debug("入座",
		slog.String("connection", fmt.Sprintf("%p", ns)),
		slog.String("座位", fmt.Sprintf("%s", game.CbSeat(seatAt))),
		slog.String("seat(Hex)", fmt.Sprintf("0x%X", seatAt)),
		slog.String("seat(Dec)", fmt.Sprintf("%d", seatAt)),
		slog.String("seat>>1", fmt.Sprintf("0x%X", seatAt>>1)),
		slog.String("status", fmt.Sprintf("%s", status)))

	ns.Emit(ClnRoomEvents.GamePrivateOnSeat, []byte{seatAt >> 1})

	// 注意
	ns.Conn.Set(game.KeySeat, game.CbSeat(seatAt))

	switch status {
	case game.SeatGetButGameWaiting:
		srv.BroadcastByte(ns, ClnRoomEvents.PlayerOnSeat, seatAt>>1)
	case game.SeatGetAndStartGame:
		//上一叫者,上一叫品
		srv.BroadcastByte(ns, ClnRoomEvents.PlayerOnSeat, seatAt>>1)
		srv.SendDeal()

		//第一個表示上一個叫者座位(因為是首叫,所以上一個叫者為valueNotSet)
		//第二個表示上一個叫者叫品CbBid(上一次叫品,因為是第一次叫所以叫品是valueNotSet)
		//第三個表示下一個叫牌者
		var payload []uint8
		payload = append(payload, GameConst.ValueNotSet, GameConst.ValueNotSet, bidder>>1)

		//延遲,是因為最後進來的前端,render速度太慢,接收到NotyBid時來不及
		time.Sleep(time.Millisecond * 700)
		//payload 第四個為禁叫品
		srv.BroadcastBytes(nil, ClnRoomEvents.GameNotyBid, append(payload, forbidden...))

		slog.Debug("叫牌",
			slog.String("bidder", fmt.Sprintf("%s", game.CbSeat(bidder))),
			slog.Any("不可叫品", forbidden[0:]))
	}
	return nil
}

// competitiveBidding todo 玩家叫牌(包含叫到第幾線,什麼叫品,誰叫的)
func (rooms AllRoom) competitiveBidding(ns *skf.NSConn, m skf.Message) error {
	var (
		srv    = rooms[m.Room]
		seat8  = uint8(ns.Conn.Get(game.KeySeat).(game.CbSeat))
		value8 uint8
		raw8   uint8
	)
	if len(m.Body) == 0 {
		err := errors.New("空叫品")
		slog.Error("競叫competitiveBidding", utilog.Err(err))
		return err
	}

	value8 = m.Body[0] //CbBid
	raw8 = value8 | seat8

	slog.Debug("競叫competitiveBidding",
		slog.String("房間", m.Room),
		slog.String("叫者", fmt.Sprintf("%s", game.CbSeat(seat8))),
		slog.String("叫者seat", fmt.Sprintf("0x%0X", seat8)),
		slog.String("叫品", fmt.Sprintf("%s", game.CbBid(value8))),
		slog.Int("叫品8", int(value8)),
		slog.String("叫品|叫者", fmt.Sprintf("0x%X", raw8)))

	go srv.BidMux(seat8, value8)
	return nil
}

func (rooms AllRoom) competitivePlaying(ns *skf.NSConn, m skf.Message) error {
	var (
		srv = rooms[m.Room]

		//Store Role於競叫底定時決定
		role   = ns.Conn.Get(game.KeyPlayRole).(game.CbRole)
		seat8  uint8 //牌真實持有者
		value8 uint8
	)

	payload := cb.PlayingCard{}
	err := pb.Unmarshal(m.Body, &payload)
	if err != nil {
		slog.Error("玩牌中competitivePlaying", utilog.Err(err))
		panic(err)
	}

	value8 = uint8(payload.CardValue)
	seat8 = uint8(payload.Seat)

	slog.Debug("❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖❖")
	slog.Debug("玩牌中competitivePlaying",
		slog.String("房間", m.Room),
		slog.String("玩家", fmt.Sprintf("%s", role)),
		slog.String("座位", fmt.Sprintf("%s", game.CbSeat(seat8))),
		slog.String("seat8(Hex)", fmt.Sprintf("0x%X", seat8)),
		slog.String("seat8(Dec)", fmt.Sprintf("%d", seat8)),
		slog.String("牌", fmt.Sprintf("%s", game.CbCard(value8))))

	go srv.PlayMux(role, seat8, value8)
	return nil
}

// callBackStoreConnectionRole 當競叫底定後,會送出訊號給各家client,各家client會回乎這個method對connection進行遊戲角色設定
// 用於設定與清除
func (rooms AllRoom) callBackStoreConnectionRole(ns *skf.NSConn, m skf.Message) error {
	slog.Warn("前端設定callBackStoreConnectionRole")
	if len(m.Body) == 0 {
		err := errors.New("連線store儲存game role,無參數,設定值是空值")
		slog.Error("前端設定callBackStoreConnectionRole", utilog.Err(err))
		panic(err)
	}
	slog.Warn("前端設定callBackStoreConnectionRole", slog.Any("store", m.Body[0]))

	//清除上局Game Role
	if m.Body[0] == GameConst.ValueNotSet {
		ns.Conn.Set(game.KeyPlayRole, game.RoleNotYet)
		slog.Warn("前端設定callBackStoreConnectionRole", slog.String("store", "清除上局Game Role"))
		return nil
	}
	slog.Warn("前端設定callBackStoreConnectionRole",
		slog.String("設定遊戲Role", fmt.Sprintf("%s", game.CbRole(m.Body[0]))),
		slog.Any("m.Body", m.Body),
		slog.Int("m.Body[0]", int(m.Body[0])))
	ns.Conn.Set(game.KeyPlayRole, game.CbRole(m.Body[0]))
	return nil
}

func (rooms AllRoom) _OnNamespaceConnected(c *skf.NSConn, m skf.Message) error {
	generalLog(c, m)

	//👍 注意,在此測試 proto buf 傳送到Client
	/*
		var msg = pb.MessagePacket{
			Type:    pb.MessagePacket_User,
			Content: "hello, Proto Message Packet - User",
			Tt:      timestamppb.New(time.Now()),
			RoomId:  1,
			From:    "Zorn",
			To:      "Sam",
		}
		any, err := anypb.New(&msg)
		if err != nil {
			panic(err)
		}
		var packet = pb.ProtoPacket{
			AnyItem: any,
			Tt:      timestamppb.Now(),
			Topic:   pb.TopicType_Message,
			SN:      0,
		}
		marshal, err := proto.Marshal(&packet)
		if err != nil {
			panic(err)
		}
		//👍
		c.Emit(skf.OnNamespaceConnected, marshal) */
	return nil
}
func (rooms AllRoom) _OnNamespaceDisconnect(c *skf.NSConn, m skf.Message) error {
	generalLog(c, m)

	ctx := context.Background()
	var err error
	err = c.LeaveAll(ctx)
	if err != nil {
		panic(err)
	}
	slog.Debug("OnNamespaceDisconnect", slog.String("namespace", m.Namespace), slog.String("status", "leave all"))

	return nil
}
func (rooms AllRoom) _OnRoomJoin(c *skf.NSConn, m skf.Message) error {
	roomLog(c, m)
	//這裡不要執行任何邏輯,因為假如這裡發生錯誤,就不會執行到 _OnRoomJoined
	//因此所有邏輯都放到 _OnRoomJoined Event中去執行
	return nil
}
func (rooms AllRoom) _OnRoomJoined(c *skf.NSConn, m skf.Message) error {
	//將加入方房間名稱存起來,在Game *Ring中或許會用到
	//c.Conn.Set("info", struct{ roomName string }{m.Room})
	roomLog(c, m)
	//注意 : 當_OnRoomJoined被觸發時一併將User放到對應Room中
	// 未來 UserJoinChannel還必須帶入玩家名稱
	if _, ok := rooms[m.Room]; !ok {
		return errors.New("無此遊戲房")
	}

	var res = rooms[m.Room].UserJoinChannel(c)

	if res != nil {
		c.Emit(ClnRoomEvents.ErrorSpace, []byte(res.Err.Error()))
		return res.Err
	}

	//測試目的地 Private
	//todo 先測試 Emit
	//針對連入者發送Private訊息👍
	c.Emit(ClnRoomEvents.Private, []byte(fmt.Sprintf("你已加入%s房間", m.Room)))
	//c.Emit(game.ClnGameEvents.Private, []byte{0x01, 0x02, 0x03, 0x04})
	//c.Emit(game.ClnGameEvents.Private, []byte{0x01})

	//todo 測試 EmitBinary 傳送 Proto
	/*
		var obj = cb.BidBoard{
			Seat:      1972,
			Forbidden: []uint8{0x1, 0xa, 0xc, 0x7f, 0x10, 0x5},
		}
		bytes, err := proto.Marshal(&obj)
		if err != nil {
			panic(err)
		}
		c.EmitBinary(game.ClnGameEvents.Private, bytes) //針對個別Connection送出 Proto
	*/

	// TODO 只對房間廣播
	//GR := rooms[m.Room]
	// todo byte👍
	//GR.BroadcastByte(game.ClnGameEvents.Private, 0x17)

	// todo bytes 👍
	//GR.BroadcastBytes(game.ClnGameEvents.Private, []byte())

	// todo string 👍
	//GR.BroadcastString(game.ClnGameEvents.Private, []byte("歡迎加入遊戲房"))

	// todo proto 👍
	/*
		var obj = cb.BidBoard{
			Seat:      1972,
			Forbidden: []uint8{0x1, 0xa, 0xc, 0x7f, 0x10, 0x5},
		}
		GR.BroadcastProto(game.ClnGameEvents.Private, &obj)
	*/

	counterService.RoomAdd(c, m.Room)
	return nil

}
func (rooms AllRoom) _OnRoomLeave(c *skf.NSConn, m skf.Message) error {
	roomLog(c, m)
	// 坑: 清除的工作不要放在這,因為假如這裡發生錯誤,那_OnRoomLeft就不會執行

	// 坑: 當Client不正常斷線時, 這裡的 *skf.NSConn就已經是 Closed了
	return nil
}
func (rooms AllRoom) _OnRoomLeft(c *skf.NSConn, m skf.Message) error {
	roomLog(c, m)
	// 只對房間廣播
	var (
		GR  = rooms[m.Room]
		res = GR.PlayerLeaveChannel(c)
	)

	//遊戲房,遊戲桌必須清空
	// 坑: 這裡不要補獲錯誤,只要記log就好
	// 若捕抓錯誤,就會發生中斷
	fmt.Println(" ⎿ PlayerLeaveChannel err:", res.Err)
	fmt.Println("   ⎿ Store release", res.Err)

	if storeSeat := c.Conn.Get(game.KeySeat); storeSeat != nil {
		c.Conn.Set(game.KeySeat, nil)
		fmt.Printf("       ⎿ KeySeat %s released\n", storeSeat)
	}
	if storeSeat := c.Conn.Get(game.KeyPlayRole); storeSeat != nil {
		c.Conn.Set(game.KeyPlayRole, nil)
		fmt.Printf("       ⎿ KeyPlayRole %s released\n", storeSeat)
	}

	res = GR.UserLeaveChannel(c)
	fmt.Println(" ⎿ UserLeaveChannel err:", res.Err)

	//GR.BroadcastBytes(game.ClnGameEvents.PlayerOnLeave, []byte("someone 離開遊戲房間"))

	c.Emit(skf.OnRoomLeft, []byte(fmt.Sprintf("已順利離開%s遊戲房", m.Room)))

	counterService.RoomSub(c, m.Room)

	time.Sleep(time.Millisecond * 300)

	slog.Info("順利離開遊戲房間", slog.String("room🏠", m.Room))
	return nil
}
