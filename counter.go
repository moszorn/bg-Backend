package project

import (
	"github.com/moszorn/pb/cb"
	"github.com/moszorn/utils/rchanr"
	"github.com/moszorn/utils/skf"
)

type (
	broadcastArg struct {
		lobbyNumOfs *cb.LobbyNumOfs
		roomNumOfs  *cb.LobbyTable
		nsConn      *skf.NSConn
		roomName    string
	}

	// Counter 負責計數進入大廳,進入房間人數,並透過BroadcastJoins,BroadcastRoomJoins channel送出給AppLobby,進行廣播
	Counter struct {

		//詢問大廳中所有房間人數資訊
		LobbyRoomsInfo rchanr.ChanReqWithArguments[struct{}, *cb.LobbyNumOfs]

		//房間-人數
		allRoomsJoins map[string]*cb.LobbyTable
		roomJoins     chan *broadcastArg //chan房間名稱表玩家加入
		roomLeaves    chan *broadcastArg //chan房間名稱表玩家離開

		lobbyLeaves chan *skf.NSConn // 代表誰進入, joins都必須調整
		lobbyJoins  chan *skf.NSConn //代表誰離開, joins都必須調整

		//廣播通知 , Lobby.go 收到後會進行大廳玩家廣播
		BroadcastJoins     chan *broadcastArg //當前大廳人數
		BroadcastRoomJoins chan *broadcastArg //某間房間人數

		//站上總人數 = 大廳人數(joiners) + 所有房間人數(roomers)
		//大廳總人數
		joiners uint32
		//所有房間人數
		roomers uint32
	}
)

//   chan <- T , <-chan T

// NewCounterService 傳入cb所有的遊戲桌名集合
func NewCounterService(roomsJoins *map[string]*cb.LobbyTable) *Counter {

	if len(*roomsJoins) == 0 {
		panic("必出傳入有效桌名集合")
	}

	idxId := int32(0)
	for roomName := range *roomsJoins {
		(*roomsJoins)[roomName] = &cb.LobbyTable{
			Name:   roomName,
			Id:     idxId,
			Joiner: 0,
		}
		idxId++
	}

	var counter = &Counter{
		LobbyRoomsInfo: make(chan rchanr.ChanRepWithArguments[struct{}, *cb.LobbyNumOfs]),
		allRoomsJoins:  *roomsJoins,
		roomJoins:      make(chan *broadcastArg),
		roomLeaves:     make(chan *broadcastArg),
		joiners:        0,
		roomers:        0,
		lobbyLeaves:    make(chan *skf.NSConn),
		lobbyJoins:     make(chan *skf.NSConn),

		BroadcastJoins:     make(chan *broadcastArg),
		BroadcastRoomJoins: make(chan *broadcastArg),
	}
	go counter.chanLoop()

	return counter
}

func (br *Counter) chanLoop() {
	//送送大廳人數

	for {
		select {
		case arg := <-br.roomJoins:
			if r, ok := br.allRoomsJoins[arg.roomName]; ok {

				br.roomers++
				r.Joiner++
				// 注意: 沒送出會掛掉
				numOfR := &cb.LobbyTable{
					Joiner: 0,
				}
				numOfR.Name = arg.roomName
				numOfR.Id = r.Id
				numOfR.Joiner = r.Joiner
				numOfR.Total = br.roomers + br.joiners
				broadcastArg := &broadcastArg{}
				broadcastArg.roomNumOfs = numOfR
				broadcastArg.nsConn = arg.nsConn

				br.BroadcastRoomJoins <- broadcastArg
			}
		case arg := <-br.roomLeaves:
			if r, ok := br.allRoomsJoins[arg.roomName]; ok {
				br.roomers--
				r.Joiner--
				// 注意: 沒送出會掛掉
				numOfR := &cb.LobbyTable{
					Joiner: 0,
				}
				numOfR.Name = arg.roomName
				numOfR.Id = r.Id
				numOfR.Joiner = r.Joiner
				numOfR.Total = br.roomers + br.joiners
				broadcastArg := &broadcastArg{}
				broadcastArg.roomNumOfs = numOfR
				broadcastArg.nsConn = arg.nsConn
				br.BroadcastRoomJoins <- broadcastArg
			}
		case nsConn := <-br.lobbyLeaves:

			br.joiners--

			// 注意: 沒送出會掛掉
			numOfL := &cb.LobbyNumOfs{
				Joiner: 0,
			}
			numOfL.Joiner = br.joiners
			numOfL.Total = br.roomers + br.joiners
			broadcastArg := &broadcastArg{}
			broadcastArg.lobbyNumOfs = numOfL
			broadcastArg.nsConn = nsConn
			br.BroadcastJoins <- broadcastArg
		case nsConn := <-br.lobbyJoins:

			br.joiners++

			// 注意: 沒送出會掛掉
			numOfL := &cb.LobbyNumOfs{
				Joiner: 0,
			}
			numOfL.Joiner = br.joiners
			numOfL.Total = br.roomers + br.joiners
			broadcastArg := &broadcastArg{}
			broadcastArg.lobbyNumOfs = numOfL
			broadcastArg.nsConn = nsConn
			br.BroadcastJoins <- broadcastArg

		case v := <-br.LobbyRoomsInfo:

			tables := make([]*cb.LobbyTable, 0, len(br.allRoomsJoins))
			for roomName := range br.allRoomsJoins {
				tables = append(tables, &cb.LobbyTable{
					Name:   roomName,
					Id:     br.allRoomsJoins[roomName].Id,
					Joiner: br.allRoomsJoins[roomName].Joiner,
				})
			}
			result := &cb.LobbyNumOfs{
				Tables: tables,                  //所有房間人數
				Joiner: br.joiners,              //大廳人數
				Total:  br.joiners + br.roomers, //站上總人數
			}
			// 注意: 沒送出會掛掉
			v.Response <- result
		default:

		}
	}
}

// GetSitePlayer 取出大廳所有房間人數資訊
func (br *Counter) GetSitePlayer() *cb.LobbyNumOfs {
	var result *cb.LobbyNumOfs

	result = br.LobbyRoomsInfo.Probe(struct{}{})
	return result
}

// LobbyAdd 進入大廳人數加1
func (br *Counter) LobbyAdd(nsConn *skf.NSConn) {
	br.lobbyJoins <- nsConn
}

// LobbySub 離開大廳,或斷線,大廳人數減一
func (br *Counter) LobbySub(nsConn *skf.NSConn) {
	br.lobbyLeaves <- nsConn
}

// RoomAdd 玩家入房間,房間人數加1
// GameSpace 玩家入房 _OnRoomJoined ,參考 manager.auth.go - _OnRoomJoined
func (br *Counter) RoomAdd(nsConn *skf.NSConn, roomName string) {
	br.roomJoins <- &broadcastArg{
		nsConn:   nsConn,
		roomName: roomName,
	}
}

// RoomSub 玩家離開房間,玩家斷線,房間人數減1
// GameSpace 玩家離房  _OnRoomLeft ,參考 manager.auth.go - _OnRoomLeft
func (br *Counter) RoomSub(nsConn *skf.NSConn, roomName string) {
	br.roomLeaves <- &broadcastArg{
		nsConn:   nsConn,
		roomName: roomName,
	}
}
