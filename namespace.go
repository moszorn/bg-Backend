package project

import (
	"context"
	"log/slog"

	"github.com/moszorn/pb/cb"
	llg "github.com/moszorn/utils/log"
	"github.com/moszorn/utils/skf"

	"project/game"
)

type (

	// eventsHandler 有多組事件處理機制(Key),每組事件有其對應的訊息處理(Value)方式,訊息處理方式是由Service函式精煉而成
	eventsHandler map[string]skf.MessageHandlerFunc

	// SpaceManager 有多個不同空間(Key),每個空間有其對不同事件處理機制(Value)
	SpaceManager map[string]eventsHandler

	// SpaceHandler interface
	//     ⎜
	//     ⎣ SpaceManager
	//       ⎿ eventsHandler
	//       ⎿ eventsHandler
	//       ⎿ eventsHandler
	SpaceHandler interface {
		spaceHandler(spaceName string) map[string]skf.MessageHandlerFunc
	}

	// CounterService 站上計數服務
	CounterService interface {
		GetSitePlayer() *cb.LobbyNumOfs
		LobbyAdd(*skf.NSConn)
		LobbySub(*skf.NSConn)
		RoomAdd(conn *skf.NSConn, roomName string)
		RoomSub(nsConn *skf.NSConn, roomName string)
	}

	// LobbyService 代表 Lobby Space , request的入口介面
	LobbyService interface {
		_OnNamespaceConnected(c *skf.NSConn, m skf.Message) error
		_OnNamespaceDisconnect(c *skf.NSConn, m skf.Message) error
		_OnRoomJoin(c *skf.NSConn, m skf.Message) error
		_OnRoomJoined(c *skf.NSConn, m skf.Message) error
		_OnRoomLeave(c *skf.NSConn, m skf.Message) error
		_OnRoomLeft(c *skf.NSConn, m skf.Message) error
	}

	// RoomService 代表 Room Space, request的入口介面
	RoomService interface {
		UserJoin(*skf.NSConn, skf.Message) error
		UserLeave(*skf.NSConn, skf.Message) error
		PlayerJoin(*skf.NSConn, skf.Message) error
		PlayerLeave(*skf.NSConn, skf.Message) error

		GamePrivateNotyBid(*skf.NSConn, skf.Message) error
		GamePrivateCardPlayClick(*skf.NSConn, skf.Message) error
		GamePrivateCardHover(*skf.NSConn, skf.Message) error
		GamePrivateFirstLead(*skf.NSConn, skf.Message) error

		_OnNamespaceConnected(*skf.NSConn, skf.Message) error
		_OnNamespaceDisconnect(*skf.NSConn, skf.Message) error
		_OnRoomJoin(*skf.NSConn, skf.Message) error
		_OnRoomJoined(*skf.NSConn, skf.Message) error
		_OnRoomLeave(*skf.NSConn, skf.Message) error
		_OnRoomLeft(*skf.NSConn, skf.Message) error
	}
)

// spaceHandler 以SpaceName找出對應的 SpaceHandler
func (spaceHandlers SpaceManager) spaceHandler(spaceName string) map[string]skf.MessageHandlerFunc {
	return spaceHandlers[spaceName]
}

var (
	//未來 房間名稱改撈db  8x7
	//cbGameRooms = []string{"room0x0", "room0x1"}
	cbGameRooms = []string{
		"room0x0", "room0x1", "room0x2", "room0x3", "room0x4", "room0x5", "room0x6", "room0x7",
		"room1x0", "room1x1", "room1x2", "room1x3", "room1x4", "room1x5", "room1x6", "room1x7",
		"room2x0", "room2x1", "room2x2", "room2x3", "room2x4", "room2x5", "room2x6", "room2x7",
		/*		"room3x0", "room3x1", "room3x2", "room3x3", "room3x4", "room3x5", "room3x6", "room3x7",
				"room4x0", "room3x1", "room4x2", "room4x3", "room4x4", "room4x5", "room4x6", "room4x7",
				"room5x0", "room5x1", "room5x2", "room5x3", "room5x4", "room5x5", "room5x6", "room5x7",
				"room6x0", "room6x1", "room6x2", "room6x3", "room6x4", "room6x5", "room6x6", "room6x7",
		*/}

	counterService    CounterService // 計數
	roomSpaceService  RoomService    // 房間
	lobbySpaceService LobbyService   // 大廳
	spaceManager      SpaceHandler   // 代表可取得eventsHandler
	Namespace         skf.Namespaces // 全域Namespace用於 skf初始
)

// initNamespace 初始化Namespace (全域變數)
func initNamespace(pid context.Context) {

	// 房間與遊戲桌
	rooms := make(map[string]*game.Game)

	// key:桌名
	tables := make(map[string]*cb.LobbyTable)
	// 設定桌名為鍵
	for idx := range cbGameRooms {
		rooms[cbGameRooms[idx]] = nil
		tables[cbGameRooms[idx]] = nil
	}

	mylog := llg.NewMyLog("app.log", slog.LevelDebug, llg.FileLog)

	counterService = NewCounterService(&tables)

	roomSpaceService = NewRoomSpaceService(pid, &rooms, counterService, mylog)

	lobbySpaceService = NewLobbySpaceService()

	spaceManager = newSpaceManager(roomSpaceService, lobbySpaceService)

	Namespace = skf.Namespaces{
		game.LobbySpaceName: spaceManager.spaceHandler(game.LobbySpaceName),
		game.RoomSpaceName:  spaceManager.spaceHandler(game.RoomSpaceName),
	}
	slog.Debug("初始化Namespace")
}

// 注入service,生成 SpaceManager
func newSpaceManager(rooms RoomService, lobby LobbyService) SpaceManager {

	roomEventHandlers := map[string]skf.MessageHandlerFunc{
		skf.OnNamespaceConnected:  rooms._OnNamespaceConnected,
		skf.OnNamespaceDisconnect: rooms._OnNamespaceDisconnect,
		skf.OnRoomJoin:            rooms._OnRoomJoin,
		skf.OnRoomJoined:          rooms._OnRoomJoined,
		skf.OnRoomLeave:           rooms._OnRoomLeave,
		skf.OnRoomLeft:            rooms._OnRoomLeft,

		game.SrvRoomEvents.UserPrivateJoin:     rooms.UserJoin,
		game.SrvRoomEvents.UserPrivateLeave:    rooms.UserLeave,
		game.SrvRoomEvents.TablePrivateOnSeat:  rooms.PlayerJoin,
		game.SrvRoomEvents.TablePrivateOnLeave: rooms.PlayerLeave,

		game.SrvRoomEvents.GamePrivateNotyBid:       rooms.GamePrivateNotyBid,
		game.SrvRoomEvents.GamePrivateFirstLead:     rooms.GamePrivateFirstLead,
		game.SrvRoomEvents.GamePrivateCardPlayClick: rooms.GamePrivateCardPlayClick,
		game.SrvRoomEvents.GamePrivateCardHover:     rooms.GamePrivateCardHover,

		//game.SrvRoomEvents.GameBid:       rooms.competitiveBidding,
		//game.SrvRoomEvents.GamePlay:      rooms.competitivePlaying,
		//game.SrvRoomEvents.GameRoleStore: rooms.callBackStoreConnectionRole,
	}

	lobbyEventHandlers := map[string]skf.MessageHandlerFunc{
		skf.OnNamespaceConnected:  lobby._OnNamespaceConnected,
		skf.OnNamespaceDisconnect: lobby._OnNamespaceDisconnect,
		skf.OnRoomJoin:            lobby._OnRoomJoin,
		skf.OnRoomJoined:          lobby._OnRoomJoined,
		skf.OnRoomLeave:           lobby._OnRoomLeave,
		skf.OnRoomLeft:            lobby._OnRoomLeft,
	}

	mg := map[string]eventsHandler{
		game.RoomSpaceName:  roomEventHandlers,
		game.LobbySpaceName: lobbyEventHandlers,
	}
	return mg
}
