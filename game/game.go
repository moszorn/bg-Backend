package game

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/moszorn/utils/skf"

	"google.golang.org/protobuf/proto"
	//"google.golang.org/protobuf/types/known/timestamppb"
)

type (
	UserCounter interface {
		RoomAdd(conn *skf.NSConn, roomName string)
		RoomSub(nsConn *skf.NSConn, roomName string)
	}
	roomUserCounter func(nsConn *skf.NSConn, roomName string)
)

type PayloadType uint8

const (
	ByteType PayloadType = iota
	ProtobufType
)

type payloadData struct {
	Player      uint8         //代表player seat 通常針對指定的玩家, 表示Zone的情境應該不會發生
	Data        []uint8       // 可以是byte, bytes
	ProtoData   proto.Message // proto
	PayloadType PayloadType   //這個 payload 屬於那種型態的封	包
}

type Game struct { // 玩家進入房間, 玩家進入遊戲,玩家離開房間,玩家離開遊戲

	Shutdown context.CancelFunc

	//計數入房間的人數,由UserCounter而設定
	CounterAdd roomUserCounter
	CounterSub roomUserCounter

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

	g := &Game{
		CounterAdd:  counter.RoomAdd,
		CounterSub:  counter.RoomSub,
		Shutdown:    cancelFunc,
		engine:      newEngine(),
		roomManager: newRoomManager(ctx),
		name:        tableName,
		Id:          tableId,
	}
	//新的一副牌
	NewDeck(g)

	g.Start()

	return g
}

// Start 啟動房間, 同時啟動RoomManager
func (g *Game) Start() {

	slog.Debug(fmt.Sprintf("Game(room:%s, roomId:%d) Start", g.name, g.Id))
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

// setEnginePlayer, Old: setEnginePlaySeat 引擎玩家更替, 新局,或換新的一局時呼叫
func (g *Game) setEnginePlayer(player uint8, next uint8) {

	// player 設定player當前玩家
	// next 下一個玩家

	//g.engine.SetCurrentSeat(player)
	//g.engine.SetNextSeat(next)
}

// --------------------- seat

// SeatShift , Old: setSeatAndGetNextPlayer 房間座位更替,新局,或換新的一局時呼叫
func (g *Game) SeatShift(seat uint8) (nextSeat uint8) {
	return g.roomManager.SeatShift(seat)
}

// start 開始遊戲,這個method會進行洗牌, bidder競叫者,forbidden競叫品, done
func (g *Game) start() (bidder uint8, forbidden []uint8, done bool) {
	//洗牌
	Shuffle(g)
	//TODO: 決定誰先開叫實作
	bidder, forbidden, done = g.engine.GetNextBid(valueNotSet, openBidding, valueNotSet)

	next := g.SeatShift(bidder)

	g.setEnginePlayer(bidder, next)

	return
}

// UserJoin 使用者進入房間,參數user必須有*skf.NSConn, userName, userZone,底層會送出 TableInfo
func (g *Game) UserJoin(user *RoomUser) {
	//TODO: 需要從engine取出當前遊戲狀態,並一併傳入roomManager.UserJoin回送給User
	// 回送給加入者訊息是RoomInfo (UserPrivateTableInfo)詢問房間人數,桌面狀態,座位狀態 (何時執行:剛進入房間時)
	go g.roomManager.UserJoin(user)
}

// UserLeave 使用者離開房間
func (g *Game) UserLeave(user *RoomUser) {
	go g.roomManager.UserLeave(user)
}

func (g *Game) PlayerJoin(user *RoomUser) {
	go g.roomManager.PlayerJoin(user)
}

func (g *Game) PlayerLeave(user *RoomUser) {
	go g.roomManager.PlayerLeave(user)
}

func (g *Game) RoomInfo(user *RoomUser) {
	go g.roomManager.RoomInfo(user)
}

/* ======================================================================================== */

// DevelopPrivatePayloadTest 測試與前端封包通訊用
func (g *Game) DevelopPrivatePayloadTest(user *RoomUser) {
	go g.roomManager.DevelopPrivatePayloadTest(user)
}

// DevelopBroadcastTest 測試與前端封包通訊用
func (g *Game) DevelopBroadcastTest(user *RoomUser) {
	go g.roomManager.DevelopBroadcastTest(user)
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

// BidMux 傳入msg.Body 表示純叫品 (byte), seat8可從NsConn.Store獲取
func (g *Game) BidMux(seat8, bid8 uint8)                                  {}
func (g *Game) PlayMux(role CbRole, seat8, play8 uint8)                   {}
func (g *Game) PlayerBid(player uint8, forbidden []uint8)                 {}
func (g *Game) allowCards(seat uint8) []uint8                             { return nil }
func (g *Game) syncPlayCard(seat uint8, playCard uint8) (sync bool)       { return false }
func (g *Game) isCardOwnByPlayer(seat uint8, playCard uint8) (valid bool) { return false }
