package project

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/moszorn/pb"
	"github.com/moszorn/pb/cb"
	utilog "github.com/moszorn/utils/log"
	"github.com/moszorn/utils/skf"
	"google.golang.org/protobuf/proto"

	"project/game"
)

/*
func NewRoomSpaceService(pid context.Context, rooms *map[string]*game.Game, counter CounterService) AllRoom {
	if len(*rooms) == 0 {
		panic("key不存在")
	}

	var roomIdSeq int32 = 1
	for roomName := range *rooms {
		(*rooms)[roomName] = game.CreateCBGame(pid, counter, roomName, roomIdSeq)
		roomIdSeq++
	}
	return *rooms
}*/

func NewRoomSpaceService(pid context.Context, rooms *map[string]*game.Game, counter CounterService, lg *utilog.MyLog) AllRoom {
	if len(*rooms) == 0 {
		panic("key不存在")
	}

	var roomIdSeq int32 = 1
	for roomName := range *rooms {
		(*rooms)[roomName] = game.CreateCBGame(lg, pid, counter, roomName, roomIdSeq)
		roomIdSeq++
	}
	return *rooms
}

// AllRoom Key: 房間名稱/Id , Value: 房間服務, AllRoom 實作 RoomService
type AllRoom map[string]*game.Game // interface should be Game

func (rooms AllRoom) room(roomName string) (roomGame *game.Game, err error) {
	var ok bool

	if len(roomName) == 0 {
		return nil, BackendError(GeneralCode, "參數不合法", nil)
	}

	if roomGame, ok = rooms[roomName]; ok {
		return roomGame, nil
	}
	return nil, BackendError(GeneralCode, "無此房間", nil)
}

func (rooms AllRoom) enterProcess(ns *skf.NSConn, m skf.Message) (g *game.Game, u *game.RoomUser, err error) {

	defer func() {
		if e := recover().(error); e != nil {
			if errors.Is(e, proto.Error) {
				slog.Error("proto嚴重錯誤", slog.String(".", err.Error()))
				//TODO
				err = e
			}
		}
	}()

	PB := &pb.PlayingUser{}
	err = pb.Unmarshal(m.Body, PB)
	if err != nil {
		panic(err)
	}

	// 提示: raw8 = seat8 | bit8
	//panic後的defer無用,所以一開始就宣告defer
	//這個Recover 主要在防止 uint8(uint32)轉型爆掉
	defer func() {
		if fatal := recover().(error); fatal != nil {
			slog.Error("嚴重錯誤", slog.String("FYI", fmt.Sprintf("name:name:%s/zone:%d/Bid:%d/Play:%d \n%s", PB.Name, PB.Zone, PB.Bid, PB.Play, fatal.Error())))
			//TODO
			err = errors.New("王八蛋不要亂搞")
		}
	}()

	//game.CbBid(u.Bid)
	u = &game.RoomUser{
		NsConn:      ns,
		PlayingUser: PB,
		Zone8:       uint8(PB.Zone), /*使用Zone8是因為可方便取用 */
		Bid8:        uint8(PB.Bid),
		Play8:       uint8(PB.Play),
		PlaySeat8:   uint8(PB.PlaySeat),
	}

	g, err = rooms.room(m.Room)
	if err != nil {
		// TBC return是不是就是會斷線
		return nil, nil, err
	}
	return
}

// UserJoin 必要參數使用者姓名, 區域
func (rooms AllRoom) UserJoin(ns *skf.NSConn, m skf.Message) (er error) {
	//roomLog(ns, m)
	g, u, er := rooms.enterProcess(ns, m)
	if er != nil {
		var err *BackendErr
		if errors.As(er, &err) {
			slog.Error("房間錯誤", slog.String("msg", err.Error()), slog.String("room", m.Room))
		}
		return
	}
	g.UserJoin(u)
	return nil
}

// UserLeave 必要參數使用者姓名, 區域
func (rooms AllRoom) UserLeave(ns *skf.NSConn, m skf.Message) (er error) {
	//roomLog(ns, m)
	g, u, er := rooms.enterProcess(ns, m)
	if er != nil {
		var err *BackendErr
		if errors.As(er, &err) {
			slog.Error("房間錯誤", slog.String("msg", err.Error()), slog.String("room", m.Room))
		}
		return
	}
	g.UserLeave(u)
	return nil
}

// PlayerJoin 必要參數使用者姓名, 區域
func (rooms AllRoom) PlayerJoin(ns *skf.NSConn, m skf.Message) (er error) {
	//roomLog(ns, m)
	g, u, er := rooms.enterProcess(ns, m)
	if er != nil {
		var err *BackendErr
		if errors.As(er, &err) {
			slog.Error("房間錯誤", slog.String("msg", err.Error()), slog.String("room", m.Room), slog.String("zone", fmt.Sprintf("%s", game.CbSeat(u.Zone8))))
		}
		return
	}
	g.PlayerJoin(u)
	return nil
}

// PlayerLeave 必要參數使用者姓名, 區域
func (rooms AllRoom) PlayerLeave(ns *skf.NSConn, m skf.Message) error {
	//roomLog(ns, m)
	g, u, er := rooms.enterProcess(ns, m)
	if er != nil {
		var err *BackendErr
		if errors.As(er, &err) {
			slog.Error("房間錯誤", slog.String("msg", err.Error()), slog.String("room", m.Room), slog.String("zone", fmt.Sprintf("%s", game.CbSeat(u.Zone8))))
		}
		return er
	}
	g.PlayerLeave(u)
	return nil
}

// GamePrivateNotyBid 玩家叫牌
func (rooms AllRoom) GamePrivateNotyBid(ns *skf.NSConn, m skf.Message) error {
	g, u, er := rooms.enterProcess(ns, m)
	if er != nil {
		var err *BackendErr
		if errors.As(er, &err) {
			slog.Error("房間錯誤", slog.String("msg", err.Error()), slog.String("room", m.Room), slog.String("zone", fmt.Sprintf("%s", game.CbSeat(u.Zone8))))
		}
		return er
	}
	//g.PlayerLeave(u)
	slog.Info("入口(GamePrivateNotyBid)",
		slog.String("FYI",
			fmt.Sprintf("叫者:%s(%s),遊戲中:%t 叫品:(%d)%s", u.Name, game.CbSeat(u.Zone8), u.IsSitting, u.Bid, game.CbBid(u.Bid))))

	go g.GamePrivateNotyBid(u)
	return nil
}

func (rooms AllRoom) GamePrivateFirstLead(ns *skf.NSConn, m skf.Message) error {
	g, u, er := rooms.enterProcess(ns, m)
	if er != nil {
		var err *BackendErr
		if errors.As(er, &err) {
			slog.Error("房間錯誤", slog.String("msg", err.Error()), slog.String("room", m.Room), slog.String("zone", fmt.Sprintf("%s", game.CbSeat(u.Zone8))))
		}
		return er
	}
	//g.PlayerLeave(u)
	slog.Info("入口(GamePrivateFirstLead)",
		slog.String("FYI",
			fmt.Sprintf("首引 %s(%s) 打出 %s  %s", u.Name, game.CbSeat(u.Zone8), game.CbSeat(u.PlaySeat8), game.CbCard(u.Play8))))

	go g.GamePrivateFirstLead(u)
	return nil
}

func (rooms AllRoom) GamePrivateCardPlayClick(ns *skf.NSConn, m skf.Message) error {
	g, u, er := rooms.enterProcess(ns, m)
	if er != nil {
		var err *BackendErr
		if errors.As(er, &err) {
			slog.Error("房間錯誤", slog.String("msg", err.Error()), slog.String("room", m.Room), slog.String("zone", fmt.Sprintf("%s", game.CbSeat(u.Zone8))))
		}
		return er
	}
	//g.PlayerLeave(u)
	slog.Info("入口(GamePrivateCardPlayClick)",
		slog.String("FYI",
			fmt.Sprintf("%s(%s) 打出 %s  %s  ", u.Name, game.CbSeat(u.Zone8), game.CbSeat(u.PlaySeat8), game.CbCard(u.Play8))))

	go g.GamePrivateCardPlayClick(u)
	return nil
}

func (rooms AllRoom) GamePrivateCardHover(ns *skf.NSConn, m skf.Message) error {

	var (
		err error
		g   *game.Game
	)
	//取出 pb.CardAction
	defer func() {
		if e := recover(); e != nil {
			slog.Error("proto嚴重錯誤(hover/hover out)", slog.Any(".", e.(error)))
		}
	}()

	cardAction := &cb.CardAction{}
	err = pb.Unmarshal(m.Body, cardAction)
	if err != nil {
		//panic(err)
		slog.Debug("GamePrivateCardHover", slog.String(".", err.Error()))
	}

	g, err = rooms.room(m.Room)
	if err != nil {
		var be *BackendErr
		if errors.As(err, &be) {
			slog.Error("房間錯誤", slog.String(".", err.Error()))
		}
	}

	go g.GamePrivateCardHover(cardAction)

	return nil
}

/*


































 */
// callBackStoreConnectionRole 當競叫底定後,會送出訊號給各家client,各家client會回乎這個method對connection進行遊戲角色設定
// 用於設定與清除
func (rooms AllRoom) callBackStoreConnectionRole(ns *skf.NSConn, m skf.Message) error {
	slog.Warn("前端設定callBackStoreConnectionRole")
	if len(m.Body) == 0 {
		err := errors.New("連線store儲存game role,無參數,設定值是空值")
		slog.Error("前端設定callBackStoreConnectionRole", slog.String(".", err.Error()))
		//panic(err)
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

func (rooms AllRoom) _OnNamespaceConnected(ns *skf.NSConn, m skf.Message) error {
	generalLog(ns, m)
	return nil
}
func (rooms AllRoom) _OnNamespaceDisconnect(c *skf.NSConn, m skf.Message) error {
	generalLog(c, m)

	ctx := context.Background()
	var err error
	err = c.LeaveAll(ctx)
	if err != nil {
		//panic(err)
		slog.Debug("_OnNamespaceDisconnect", slog.String(".", err.Error()))

	}
	return nil
}
func (rooms AllRoom) _OnRoomJoin(c *skf.NSConn, m skf.Message) error {
	//generalLog(c, m)
	//這裡不要執行任何邏輯,因為假如這裡發生錯誤,就不會執行到 _OnRoomJoined
	//因此所有邏輯都放到 _OnRoomJoined Event中去執行
	return nil
}

// Message中必須要有玩家姓名
func (rooms AllRoom) _OnRoomJoined(ns *skf.NSConn, m skf.Message) error {
	generalLog(ns, m)
	//g, u, er := rooms.enterProcess(ns, m)
	g, u, er := rooms.enterProcess(ns, m)
	if er != nil {
		var err *BackendErr
		if errors.As(er, &err) {
			slog.Error("房間錯誤", slog.String("msg", err.Error()), slog.String("room", m.Room))
		}
		return er
	}

	//底下在測試封包傳送
	//g.DevelopBroadcastTest(u)
	//g.DevelopPrivatePayloadTest(u)

	//送出桌面座位順序,觀眾資訊
	g.UserJoinTableInfo(u)

	return nil
}

// 前端必須曾經執行過  socket.emit(skf.OnRoomJoin); _OnRoomLeft才會生效
// _OnRoomLeave先執行後才執行_OnRoomLeft
func (rooms AllRoom) _OnRoomLeft(c *skf.NSConn, m skf.Message) error {
	roomLog(c, m)
	g, _, er := rooms.enterProcess(c, m)
	if er != nil {
		var err *BackendErr
		if errors.As(er, &err) {
			slog.Error("房間錯誤", slog.String("msg", err.Error()), slog.String("room", m.Room))
		}
		return er
	}

	// 表示Client在房間裡突然斷線,仍殘留在房間紀錄,所以這裡是做最後檢查
	if c.Conn.Get(game.KeyRoom) != nil || c.Conn.Get(game.KeyGame) != nil {
		//不正常斷線時 Message是沒有任何資料的
		slog.Debug("_OnRoomLeft不❌正常離開", slog.String("連線", c.String()))
		go g.KickOutBrokenConnection(c)
	}

	//前端必須接到後才能變scene

	return nil
}

// 前端必須曾經執行過  socket.emit(skf.OnRoomJoin); _OnRoomLeave才會生效
// _OnRoomLeave先執行後才執行_OnRoomLeft
func (rooms AllRoom) _OnRoomLeave(c *skf.NSConn, m skf.Message) error {
	//roomLog(c, m)
	// 坑: 清除的工作不要放在這,因為假如這裡發生錯誤,那_OnRoomLeft就不會執行

	// 坑: 當Client不正常斷線時, 這裡的 *skf.NSConn就已經是 Closed了
	return nil
}
