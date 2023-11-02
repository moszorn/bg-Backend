package project

//go:generate stringer -type=ServerClientEnum --linecomment -output namespace.enum_strings.go

const (
	LobbySpaceName = "lobbySpace"
	RoomSpaceName  = "cbSpace"
)

type ServerClientEnum byte

const (
	serverEvent ServerClientEnum = iota //server
	clientEvent                         // client
)

type (
	lobbyNamespace struct {
		NumOfUsers       string `json:"numOfUsers,omitempty"`       //大廳人數
		NumOfRooms       string `json:"numOfRooms,omitempty"`       //大廳房間
		NumOfUsersInRoom string `json:"numOfUsersInRoom,omitempty"` //某特定房間人數
		NumOfUsersOnSite string `json:"numOfUsersOnSite,omitempty"` //包含大廳人數,與所有房間人數
	}

	// 屬性名稱是PrivateXxxx表示是通知個人私人訊號否則是大眾廣播訊號
	roomNamespace struct {
		UserJoinRoom  string `json:"userJoinRoom,omitempty"`
		UserLeaveRoom string `json:"userLeaveRoom,omitempty"`

		TablePrivateOnSeat string `json:"tablePrivateOnSeat,omitempty"` //Done (私人)
		TableOnSeat        string `json:"tableOnSeat,omitempty"`        //Done (廣播)

		TablePrivateOnLeave string `json:"tablePrivateOnLeave,omitempty"` //Done (私人)
		TableOnLeave        string `json:"tableOnLeave,omitempty"`        //Done (廣播)

		NamespaceCommon string `json:"namespaceCommon,omitempty"`
		Private         string `json:"private,omitempty"` //Done

		//遊戲開始發牌事件(clientEvent Only)
		GamePrivateDeal string `json:"gamePrivateDeal,omitempty"` //Done (私人)
		GameDeal        string `json:"gameDeal,omitempty"`        //Done (廣播)

		//私人訊息:玩家座位
		GamePrivateOnSeat string `json:"gamePrivateOnSeat,omitempty"`

		//競叫起叫(起叫)是一個特殊事件,前端必須特別處理,其他競叫就是一般作法 (clientEvent only)
		GameOpenBidStart string `json:"gameOpenBidStart,omitempty"`

		//競叫中
		GameBid             string `json:"gameBid,omitempty"`
		GamePlay            string `json:"gamePlay,omitempty"`
		GameRoleStore       string `json:"gameRoleStore,omitempty"`
		GameCardsConstraint string `json:"gameCardsConstraint,omitempty"`

		//通知
		GamePrivateNotyBid     string `json:"gamePrivateNotyBid,omitempty"` //Done (私人)
		GameNotyBid            string `json:"gameNotyBid,omitempty"`        //Done (廣播)
		GameNotyFirstLead      string `json:"gameNotyFirstLead,omitempty"`
		GameNotyGameReshuffle  string `json:"gameNotyGameReshuffle,omitempty"`
		GameNotyDummy          string `json:"gameNotyDummy,omitempty"`
		GameNotyResult         string `json:"gameNotyResult,omitempty"`
		GameNotyNext           string `json:"gameNotyNext,omitempty"`
		GameNotyAutoPlay       string `json:"gameNotyAutoPlay,omitempty"`
		GameNotyCardRefresh    string `json:"gameNotyCardRefresh,omitempty"`
		GameNotyClearGameTable string `json:"gameNotyClearGameTable,omitempty"`

		//首引後,莊家牌顯示給夢家看
		GameNotyShowDeclarerHand string `json:"gameNotyShowDeclarerHand,omitempty"`

		//接收Space時發生錯誤的回覆
		ErrorSpace string `json:"errorSpace,omitempty"` //Done
		//接收Room時發生錯誤的回覆
		ErrorRoom string `json:"errorRoom,omitempty"`
		//接收Game時發生錯誤的回覆
		ErrorGame string `json:"errorGame,omitempty"`
	}
)

var (
	/*************** LobbySpaceName setting *******************************/
	// client請求Server時要註明哪一個Server Event handler做服務
	serverLobbySpace = &lobbyNamespace{
		/*暫無*/
	}

	// server回覆Client時要註明哪一個Client Event handler為接收
	clientLobbySpace = &lobbyNamespace{
		NumOfUsers:       "default.numOfUsers",
		NumOfRooms:       "default.numOfRooms",
		NumOfUsersInRoom: "default.numOfUsersInRoom",
		NumOfUsersOnSite: "default.allUsers",
	}

	lobbySpaceEvents = map[ServerClientEnum]*lobbyNamespace{
		serverEvent: serverLobbySpace,
		clientEvent: clientLobbySpace,
	}

	SrvLobbyEvents = lobbySpaceEvents[serverEvent]
	ClnLobbyEvents = lobbySpaceEvents[clientEvent]

	/*************** GameNamespace setting *******************************/
	//client -> server
	serverRoomSpace = &roomNamespace{
		UserJoinRoom:        "以_OnRoomJoined代替",
		UserLeaveRoom:       "UserLeaveRoom",
		NamespaceCommon:     "cb.common",
		TableOnLeave:        "TableOnLeave",
		TablePrivateOnLeave: "TablePrivateOnLeave",
		TableOnSeat:         "TableOnSeat",
		TablePrivateOnSeat:  "TablePrivateOnSeat",

		GameBid:       "game.bid",
		GamePlay:      "game.play",
		GameRoleStore: "game.role",
	}
	// server -> client
	clientRoomSpace = &roomNamespace{
		UserJoinRoom:        "UserJoinRoom",
		UserLeaveRoom:       "UserLeaveRoom",
		NamespaceCommon:     "cb.common",
		TableOnLeave:        "table.leave",     //Done
		TablePrivateOnLeave: "table.p.leave",   //Done
		TableOnSeat:         "table.seat",      //Done
		TablePrivateOnSeat:  "table.p.seat",    //Done
		Private:             "private",         // Done
		GamePrivateDeal:     "game.p.deal",     //Done
		GameDeal:            "game.deal",       //Done
		GameNotyBid:         "game.noty.bid",   // Done
		GamePrivateNotyBid:  "game.p.noty.bid", //Done

		GamePrivateOnSeat:        "game.start.seat",
		GameOpenBidStart:         "game.start.bid",
		GameBid:                  "game.bid",
		GamePlay:                 "game.play",
		GameRoleStore:            "game.role",
		GameNotyFirstLead:        "game.noty.lead",
		GameNotyGameReshuffle:    "game.noty.reshuffle",
		GameNotyDummy:            "game.noty.dummy",
		GameNotyResult:           "game.noty.result",
		GameNotyNext:             "game.noty.next",
		GameNotyAutoPlay:         "game.noty.autoplay",
		GameNotyCardRefresh:      "game.noty.card.refresh",
		GameCardsConstraint:      "game.constraint.cards",
		GameNotyShowDeclarerHand: "game.noty.declarer",
		GameNotyClearGameTable:   "game.noty.cln.table",
		ErrorSpace:               "err.space",
		ErrorRoom:                "err.room",
		ErrorGame:                "err.game",
	}

	// Game 命名空間
	// 透過side enum找出對應(server side/client side) "[遊戲]命名空間事件名稱集合"
	roomSpaceEvents = map[ServerClientEnum]*roomNamespace{
		serverEvent: serverRoomSpace,
		clientEvent: clientRoomSpace,
	}
	// SrvRoomEvents serverEvent side Game RoomSpaceName 事件名稱們
	SrvRoomEvents = roomSpaceEvents[serverEvent]

	// ClnRoomEvents clientEvent side Game RoomSpaceName 事件名稱們
	ClnRoomEvents = roomSpaceEvents[clientEvent]

	/*************** todo Room(Table) setting *******************************/
)

// TODO: 可以先轉成Json再轉成 proto JSON 傳向前端
func GetNamespaceJson() {
	// 取得Lobby Namespace Json 轉成 Dartlang Class
	//bys, err := json.Marshal(lobbySpaceEvents[clientEvent])
	//bys, err := json.Marshal(clientLobbySpace)

	// 取得Room Namespace Json 轉成 Dartlang Class
	//bys, err := json.Marshal(roomSpaceEvents[clientEvent])
	//bys, err := json.Marshal(clientRoomSpace)

	//fmt.Printf("%s\n", string(bys))
}
