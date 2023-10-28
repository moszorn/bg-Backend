package project

import (
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

	// SpaceHandler 由 SpaceManager實作
	SpaceHandler interface {
		spaceHandler(spaceName string) map[string]skf.MessageHandlerFunc
	}

	LobbyService interface {
		_OnNamespaceConnected(c *skf.NSConn, m skf.Message) error
		_OnNamespaceDisconnect(c *skf.NSConn, m skf.Message) error
	}

	RoomService interface {
		userLeaveRoom(ns *skf.NSConn, m skf.Message) error
		playerOnLeave(ns *skf.NSConn, m skf.Message) error
		playerOnSeat(ns *skf.NSConn, m skf.Message) error
		competitiveBidding(ns *skf.NSConn, m skf.Message) error
		competitivePlaying(ns *skf.NSConn, m skf.Message) error
		callBackStoreConnectionRole(ns *skf.NSConn, m skf.Message) error

		_OnNamespaceConnected(c *skf.NSConn, m skf.Message) error
		_OnNamespaceDisconnect(c *skf.NSConn, m skf.Message) error
		_OnRoomJoin(c *skf.NSConn, m skf.Message) error
		_OnRoomJoined(c *skf.NSConn, m skf.Message) error
		_OnRoomLeave(c *skf.NSConn, m skf.Message) error
		_OnRoomLeft(c *skf.NSConn, m skf.Message) error
	}
)

func (smgr SpaceManager) spaceHandler(spaceName string) map[string]skf.MessageHandlerFunc {
	return smgr[spaceName]
}

func newSpaceManager(rooms RoomService, lobby LobbyService) SpaceManager {

	roomEventHandlers := map[string]skf.MessageHandlerFunc{
		skf.OnNamespaceConnected:    rooms._OnNamespaceConnected,
		skf.OnNamespaceDisconnect:   rooms._OnNamespaceDisconnect,
		skf.OnRoomJoin:              rooms._OnRoomJoin,
		skf.OnRoomJoined:            rooms._OnRoomJoined,
		skf.OnRoomLeave:             rooms._OnRoomLeave,
		skf.OnRoomLeft:              rooms._OnRoomLeft,
		SrvRoomEvents.PlayerOnSeat:  rooms.playerOnSeat,
		SrvRoomEvents.PlayerOnLeave: rooms.playerOnLeave,
		SrvRoomEvents.UserLeaveRoom: rooms.userLeaveRoom,
		SrvRoomEvents.UserLeaveRoom: rooms.userLeaveRoom,
		SrvRoomEvents.GameBid:       rooms.competitiveBidding,
		SrvRoomEvents.GamePlay:      rooms.competitivePlaying,
		SrvRoomEvents.GameRoleStore: rooms.callBackStoreConnectionRole,
	}

	lobbyEventHandlers := map[string]skf.MessageHandlerFunc{
		skf.OnNamespaceConnected:  lobby._OnNamespaceConnected,
		skf.OnNamespaceDisconnect: lobby._OnNamespaceDisconnect,
	}

	mg := map[string]eventsHandler{
		RoomSpaceName:  roomEventHandlers,
		LobbySpaceName: lobbyEventHandlers,
	}

	return mg
}

var (
	//未來 房間名稱改撈db
	cbRooms = []string{"room1", "room2"}

	counterService    CounterService
	roomSpaceService  RoomService
	lobbySpaceService LobbyService
	spaceManager      SpaceHandler
	Namespace         skf.Namespaces
)

// 初始化Namespace
func initNamespace() {

	rooms := make(map[string]*game.Game)
	roomsCounter := make(map[string]*cb.LobbyTable)

	for idx := range cbRooms {
		rooms[cbRooms[idx]] = nil
		roomsCounter[cbRooms[idx]] = nil
	}

	counterService = NewCounterService(&roomsCounter)
	roomSpaceService = NewRoomSpaceService(&rooms)

	lobbySpaceService = NewLobbySpaceService()

	spaceManager = newSpaceManager(roomSpaceService, lobbySpaceService)

	Namespace = skf.Namespaces{
		"52.cb.lobby": spaceManager.spaceHandler(LobbySpaceName),
		"52.cb.room":  spaceManager.spaceHandler(RoomSpaceName),
	}
	slog.Debug("初始化Namespace")
}
