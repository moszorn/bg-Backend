package project

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/moszorn/pb"
	"github.com/moszorn/pb/cb"
	"github.com/moszorn/utils/skf"
	"project/game"
)

type (
	One struct {
		set  *int32
		init int32
	}

	BridgeGameLobby struct {
		one    *One        //專用設定 BridgeGameLobby.server
		server *skf.Server //第一個client連線進來時,從client NSConn.Conn.serverEvent keep server

		//進出大廳人數計數委派LobbyRooms負責,在chanLoop中監聽是否人數異動並廣播
		counter *Counter

		IsStart bool
	}
)

func newOnce(v int32) *One {
	return &One{
		new(int32),
		v,
	}
}
func (o *One) touch(f func()) {
	if swapped := atomic.CompareAndSwapInt32(o.set, o.init, o.init); swapped {
		f()
		o.init++
	}
}

func NewLobbySpaceService() *BridgeGameLobby {
	var appLobby = &BridgeGameLobby{
		one:     newOnce(0),
		server:  nil,
		counter: counterService.(*Counter),
	}
	go appLobby.chanLoop()
	appLobby.IsStart = true
	return appLobby
}

func (app *BridgeGameLobby) chanLoop() {

	for {
		select {
		case arg := <-app.counter.BroadcastRoomJoins:

			slog.Debug("廣播房間人數",
				slog.Int("roomId", int(arg.roomNumOfs.Id)),
				slog.String("room", arg.roomNumOfs.Name),
				slog.Int("大廳人數", int(arg.roomNumOfs.Joiner)),
				slog.Int("站上總數", int(arg.roomNumOfs.Total)))

			msg := skf.Message{
				Namespace: game.LobbySpaceName,
				SetBinary: true,
			}

			msg.Event = game.ClnLobbyEvents.NumOfUsersInRoom
			//送出 cb.LobbyTable
			msg.Body, _ = pb.Marshal(arg.roomNumOfs)

			//注意: 這裡有可能在廣播時掛掉嗎?
			app.server.Broadcast(arg.nsConn, msg)

		case arg := <-app.counter.BroadcastJoins:

			slog.Debug("廣播大廳人數",
				slog.Int("大廳人數", int(arg.lobbyNumOfs.Joiner)),
				slog.Int("總人數", int(arg.lobbyNumOfs.Total)))

			msg := skf.Message{
				Namespace: game.LobbySpaceName,
				SetBinary: true,
			}
			msg.Event = game.ClnLobbyEvents.NumOfUsersOnSite
			//送出 cb.LobbyNumOfs
			msg.Body, _ = pb.Marshal(arg.lobbyNumOfs)
			app.server.Broadcast(arg.nsConn, msg)
		}
	}
}

func (app *BridgeGameLobby) connectServer(c *skf.NSConn) {
	// 從第一個連線Conn中取得Server,以方便後續Lobby對所有Namespace的廣播
	app.one.touch(func() { app.server = c.Conn.Server() })
}

func (app *BridgeGameLobby) _OnNamespaceConnected(c *skf.NSConn, m skf.Message) error {
	generalLog(c, m)

	//只有第一個Request時才會有效執行
	app.connectServer(c)

	//step1.大廳人數加加,並對已經在大廳的人進行廣播(app.counter.BroadcastJoins)
	app.counter.LobbyAdd(c)

	//step2. 對剛連上的Client,個別送出大廳房間人數資訊
	var l cb.LobbyNumOfs
	l = *app.counter.GetSitePlayer()

	marshal, err := pb.Marshal(&l)
	if err != nil {
		panic(err)
	}

	// 坑: 透過 c.EmitBinary 前端想要讀出,必須參考 message.d.dart C A T C H  FORMAT:294
	c.EmitBinary(game.ClnLobbyEvents.NumOfRooms, marshal)

	return nil
}

func (app *BridgeGameLobby) _OnNamespaceDisconnect(c *skf.NSConn, m skf.Message) error {
	generalLog(c, m)

	app.counter.LobbySub(c)

	ctx := context.Background()
	var err error
	err = c.LeaveAll(ctx)
	if err != nil {
		panic(err)
	}

	return nil
}

func (app *BridgeGameLobby) _OnRoomJoin(c *skf.NSConn, m skf.Message) error {
	generalLog(c, m)
	return nil
}
func (app *BridgeGameLobby) _OnRoomJoined(c *skf.NSConn, m skf.Message) error {
	generalLog(c, m)
	return nil
}
func (app *BridgeGameLobby) _OnRoomLeave(c *skf.NSConn, m skf.Message) error {
	generalLog(c, m)
	return nil
}
func (app *BridgeGameLobby) _OnRoomLeft(c *skf.NSConn, m skf.Message) error {
	generalLog(c, m)
	return nil
}
