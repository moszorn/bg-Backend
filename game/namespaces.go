package game

//go:generate stringer -type=ServerClientEnum --linecomment -output namespace.enum_strings.go

const (
	LobbySpaceName = "52.cb.lobby"
	RoomSpaceName  = "52.cb.room"
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
		UserPrivateTableInfo string `json:"userPrivateTableInfo,omitempty"`
		UserPrivateJoin      string `json:"userPrivateJoin,omitempty"` //Done (私人)
		UserJoin             string `json:"userJoin,omitempty"`        //Done (廣播)

		UserPrivateLeave string `json:"userPrivateLeave,omitempty"` //Done (私人)
		UserLeave        string `json:"userLeave,omitempty"`        //Done (廣播)

		TablePrivateOnSeat string `json:"tablePrivateOnSeat,omitempty"` //Done (私人)
		TableOnSeat        string `json:"tableOnSeat,omitempty"`        //Done (廣播)

		TablePrivateOnLeave string `json:"tablePrivateOnLeave,omitempty"` //Done (私人)
		TableOnLeave        string `json:"tableOnLeave,omitempty"`        //Done (廣播)

		Private string `json:"private,omitempty"` //Done
		//遊戲開始發牌事件(clientEvent Only)
		GamePrivateDeal string `json:"gamePrivateDeal,omitempty"` //Done (私人)
		GameDeal        string `json:"gameDeal,omitempty"`        //Done (廣播)

		GamePrivateNotyBid string `json:"gamePrivateNotyBid,omitempty"` //Done (私人)
		GameNotyBid        string `json:"gameNotyBid,omitempty"`        //Done (廣播)

		DevelopPrivatePayloadTest string `json:"developPrivatePayloadTest,omitempty"` //Done (私人)
		DevelopPayloadTest        string `json:"developPayloadTest,omitempty"`        //Done (廣播)

		DevelopBroadcastTest string `json:"developBroadcastTest,omitempty"`
		/* ------------------------------------------------------------------------ */
		NamespaceCommon string `json:"namespaceCommon,omitempty"`
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
		NumOfUsers:       "cnou",
		NumOfRooms:       "cnor", // d.numOfRooms
		NumOfUsersInRoom: "cnouir",
		NumOfUsersOnSite: "cnouos",
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
		UserPrivateJoin:     "upj",  //Done
		UserPrivateLeave:    "upl",  //Done
		TablePrivateOnLeave: "tpol", //Done
		TablePrivateOnSeat:  "tpos", //Done

		NamespaceCommon: "cb.common",
		GameBid:         "game.bid",
		GamePlay:        "game.play",
		GameRoleStore:   "game.role",
	}
	// server -> client
	clientRoomSpace = &roomNamespace{
		UserPrivateTableInfo:      "upti",
		UserJoin:                  "uj",
		UserLeave:                 "ul",
		NamespaceCommon:           "cb.common",
		TableOnLeave:              "tol",     //Done
		TablePrivateOnLeave:       "tpol",    //Done
		TableOnSeat:               "tos",     //Done
		TablePrivateOnSeat:        "tpos",    //Done
		Private:                   "private", // Done
		GamePrivateDeal:           "gpd",     //Done
		GameDeal:                  "gd",      //Done
		GameNotyBid:               "gnb",     // Done
		GamePrivateNotyBid:        "gpnb",    //Done
		DevelopPayloadTest:        "dpt",     //Done
		DevelopPrivatePayloadTest: "dppt",    //Done
		DevelopBroadcastTest:      "dbt",

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
		ErrorSpace:               "e.space",
		ErrorRoom:                "e.room",
		ErrorGame:                "e.game",
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
