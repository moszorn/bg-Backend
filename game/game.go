package game

import (
	"context"

	"github.com/moszorn/pb"
	"github.com/moszorn/utils/skf"
)

type (
	UserCounter interface {
		RoomAdd(conn *skf.NSConn, roomName string)
		RoomSub(nsConn *skf.NSConn, roomName string)
	}
	roomUserCounter func(*skf.NSConn, string)
)

type PayloadType uint8

const (
	ByteType PayloadType = iota
	ProtobufType
)

type payloadData struct {
	Player      uint8       //代表player seat 通常針對指定的玩家, 表示Zone的情境應該不會發生
	Data        []uint8     // 可以是byte, bytes
	ProtoData   pb.Message  // proto
	PayloadType PayloadType //這個 payload 屬於那種型態的封	包
}

type Game struct { // 玩家進入房間, 玩家進入遊戲,玩家離開房間,玩家離開遊戲

	Shutdown context.CancelFunc

	//計數入房間的人數
	counterAdd roomUserCounter
	counterSub roomUserCounter

	// 未來 當遊戲桌關閉時,記得一同關閉channel 以免leaking
	roomManager *RoomManager //管理遊戲房間所有連線(觀眾,玩家),與當前房間(Game)中的座位狀態
	engine      *Engine

	roundSuitKeeper *RoundSuitKeep

	// Key: Ring裡的座位指標(SeatItem.Name), Value:牌指標
	// 並且同步每次出牌結果(依照是哪一家打出什牌並該手所打出的牌設成0指標
	Deck map[*uint8][]*uint8
	//遊戲中各家的持牌,會同步手上的出牌,打出的牌會設成0x0 CardCover
	deckInPlay map[uint8]*[NumOfCardsOnePlayer]uint8

	//代表遊戲中一副牌,從常數集合複製過來,參:dealer.NewDeck
	deck [NumOfCardsInDeck]*uint8

	//在_OnRoomJoined階段,透過 Game.userJoin 加入Users
	___________Users    map[*RoomUser]struct{} // 進入房間者們 Key:玩家座標  value:玩家入桌順序.  一桌只限50人
	___________ticketSN int                    //目前房間人數流水號,從1開始

	name string // room(房間)/table(桌)/遊戲名稱
	Id   int32  // room(房間)/table(桌)/遊戲 Id

	// 遊戲進行中出牌數計數器,當滿52張出牌表示遊戲局結算,遊戲結束
	countingInPlayCard uint8
}

// CreateCBGame 建立橋牌(Contract Bridge) Game
func CreateCBGame(pid context.Context, counter UserCounter, tableName string, tableId int32) *Game {

	ctx, cancelFunc := context.WithCancel(pid)

	cbGame := &Game{
		counterAdd:  counter.RoomAdd,
		counterSub:  counter.RoomSub,
		Shutdown:    cancelFunc,
		engine:      newEngine(),
		roomManager: newRoomManager(ctx),
		name:        tableName,
		Id:          tableId,
	}
	//新的一副牌
	NewDeck(cbGame)

	cbGame.Start()

	return cbGame
}

// Start 啟動房間, 同時啟動RoomManager
func (g *Game) Start() {

	g.roomManager.g = g

	go g.roomManager.Start() //啟動RoomManager
}

// Close 關閉關閉, 同時關閉RoomManager
func (g *Game) Close() {
	//關閉RoomManager資源
	g.Shutdown()

	//TODO 釋放與Game有關的資源
	// ... goes here
}

// ----------------------engine

// setEnginePlayer, Old: setEnginePlaySeat 引擎玩家更替
func (g *Game) setEnginePlayer(player uint8, next uint8) {

	// player 設定player當前玩家
	// next 下一個玩家

	//g.engine.SetCurrentSeat(player)
	//g.engine.SetNextSeat(next)
}

// --------------------- seat

// SeatShift , Old: setSeatAndGetNextPlayer 房間座位更替
func (g *Game) SeatShift(seat uint8) (nextSeat uint8) {
	return g.roomManager.SeatShift(seat)
}

// start 開始遊戲
func (g *Game) start() (bidder uint8, forbidden []uint8, done bool) {
	//洗牌
	Shuffle(g)
	//TODO: 決定誰先開叫實作
	bidder, forbidden, done = g.engine.GetNextBid(valueNotSet, openBidding, valueNotSet)

	next := g.SeatShift(bidder)

	g.setEnginePlayer(bidder, next)

	return
}

// UserJoin 使用者進入房間
func (g *Game) UserJoin(ns *skf.NSConn, userName string, userZone uint8) {
	go g.roomManager.UserJoin(ns, userName, userZone)
}

// UserLeave 使用者離開房間
func (g *Game) UserLeave(ns *skf.NSConn, userName string, userZone uint8) {
	go g.roomManager.UserLeave(ns, userName, userZone)
}

func (g *Game) PlayerJoin(ns *skf.NSConn, userName string, userZone uint8) {
	go g.roomManager.PlayerJoin(ns, userName, userZone)
}

func (g *Game) PlayerLeave(ns *skf.NSConn, userName string, userZone uint8) {
	go g.roomManager.PlayerLeave(ns, userName, userZone)
}

/* ======================================================================================== */
/* ======================================================================================== */
/* ======================================================================================== */
/* ======================================================================================== */
/* ======================================================================================== */
/* ======================================================================================== */
/* ======================================================================================== */
/* ======================================================================================== */
/* ======================================================================================== */
/* ======================================================================================== */

// BidMux 傳入msg.Body 表示純叫品 (byte), seat8可從NsConn.Store獲取
func (g *Game) BidMux(seat8, bid8 uint8)                                  {}
func (g *Game) PlayMux(role CbRole, seat8, play8 uint8)                   {}
func (g *Game) PlayerBid(player uint8, forbidden []uint8)                 {}
func (g *Game) allowCards(seat uint8) []uint8                             { return nil }
func (g *Game) syncPlayCard(seat uint8, playCard uint8) (sync bool)       { return false }
func (g *Game) isCardOwnByPlayer(seat uint8, playCard uint8) (valid bool) { return false }
