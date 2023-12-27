package project

import (
	"context"
	"log/slog"

	"github.com/moszorn/pb/cb"
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
	//未來 房間名稱改撈db
	cbGameRooms = []string{"room0x0", "room0x1"}

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

	counterService = NewCounterService(&tables)

	roomSpaceService = NewRoomSpaceService(pid, &rooms, counterService)

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
