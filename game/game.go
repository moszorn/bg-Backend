package game

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/moszorn/pb/cb"
	utilog "github.com/moszorn/utils/log"
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

func (g *Game) Name() string {
	return g.name
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

// 設定當家,結算該回合比牌時會用到
func (g *Game) setEnginePlayer(player uint8) {
	// player 設定player當前玩家, 比牌算牌時,需要知道current seat
	g.engine.SetCurrentSeat(player)
}

// --------------------- seat

// SeatShift , 座位更替,新局時呼叫
func (g *Game) SeatShift(seat uint8) (nextSeat uint8) {
	return g.roomManager.SeatShift(seat)
}

// start 開始遊戲,這個method會進行洗牌, bidder競叫者,limitBiddingValue 禁叫品
func (g *Game) start() (currentPlayer, limitBiddingValue uint8) {
	//洗牌
	Shuffle(g)

	// limitBiddingValue 必定是 zeroBid ,因此 重要 前端必須判斷開叫是否是首叫狀態
	currentPlayer, limitBiddingValue = g.engine.StartBid()

	//設定Engine當前玩家與下一個玩家

	//step1. 設定位置環形
	//nextPlayer := g.SeatShift(currentPlayer)

	//設定引擎
	//g.setEnginePlayer(nextPlayer)

	return
}

func (g *Game) KickOutBrokenConnection(ns *skf.NSConn) {
	go g.roomManager.KickOutBrokenConnection(ns)
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

func (g *Game) UserJoinTableInfo(user *RoomUser) {
	go g.roomManager.UserJoinTableInfo(user)
}

func (g *Game) _(user *RoomUser) {
	/*
		winner := g.engine.GetPlayResult()
		go g.roomManager.broadcast(winner)
	*/
}

/*
	//移動環形,並校準座位
	nextPlayer := g.SeatShift(currentPlayer)
	g.setEnginePlayer(currentPlayer, nextPlayer)

	//TODO 未來 工作
	//以首引生成 RoundSuit keep
	//g.roundSuitKeeper = NewRoundSuitKeep(leadPlayer)
*/
//
func (g *Game) GamePrivateNotyBid(currentBidder *RoomUser) error {

	nextLimitBidding, db1, db2 := g.engine.GetNextBid(currentBidder.Zone8, currentBidder.Bid8)

	g.setEnginePlayer(currentBidder.Zone8)

	//移動環形,並校準座位
	next := g.SeatShift(currentBidder.Zone8)

	complete, needReBid := g.engine.IsBidFinishedOrReBid()
	switch complete {
	case false: //仍在競叫中
		//第一個參數: 表示下一個開叫牌者 前端(Player,觀眾席)必須處理
		//第二個參數: 禁叫品項,因為是首叫所以禁止叫品是 重要 zeroBid 前端(Player,觀眾席)必須處理
		//第三個參數: 上一個叫牌者
		//第四個參數: 上一次叫品
		g.roomManager.sendBytesToPlayers(append([]uint8{},
			next,
			nextLimitBidding,
			currentBidder.Zone8,
			currentBidder.Bid8,
			db1.value,
			db1.isOn,
			db2.value,
			db2.isOn), ClnRoomEvents.GamePrivateNotyBid)

	case true: //競叫完成
		fmt.Printf("競叫完成之- needReBid: %t\n", needReBid)
		switch needReBid {
		case true: //重新洗牌,重新競叫

			//清除叫牌紀錄
			g.engine.ClearBiddingState()

			//現出另三家的底牌,三秒後在重新發新牌
			g.roomManager.SendPlayersHandDeal()
			time.Sleep(time.Second * 3)

			// StartOpenBid會更換新一局,因此玩家順序也做了更動
			bidder, _ := g.start()

			//前端重新叫訊號
			reBidSignal := valueNotSet

			//重發牌
			g.roomManager.SendDeal()

			//送出reBid
			g.roomManager.sendBytesToPlayers(append([]uint8{},
				bidder,
				valueNotSet,
				reBidSignal,
				valueNotSet,
				uint8(Db1),
				uint8(0),
				uint8(Db1x2),
				uint8(0)),
				ClnRoomEvents.GamePrivateNotyBid)

		case false: //競叫完成,遊戲開始

			lead, declarer, dummy, suit, finallyBidding, err := g.engine.GameStartPlayInfo()

			if err != nil {
				if errors.Is(err, ErrUnContract) {
					slog.Error("GamePrivateNotyBid", slog.String("FYI", fmt.Sprintf("合約有問題,只能在合約確定才能呼叫GameStartPlayInfo,%s", utilog.Err(err))))
					return err
				}
			}

			g.engine.ClearBiddingState()

			//TODO 未來 工作
			//以首引生成 RoundSuit keep
			//g.roundSuitKeeper = NewRoundSuitKeep(leadPlayer)

			//nextPlayer := g.SeatShift(leadPlayer)
			//g.setEnginePlayer(leadPlayer, nextPlayer)

			//送出首引封包
			// 封包位元依序為:首引, 莊家, 夢家, 合約王牌,王牌字串, 合約線位, 線位字串
			contractPayload := cb.Contract{
				Lead:           uint32(lead),
				Declarer:       uint32(declarer),
				Dummy:          uint32(dummy),
				Suit:           uint32(suit),
				Contract:       uint32(finallyBidding.contract),
				SuitString:     fmt.Sprintf("%s", CbSuit(suit)),
				ContractString: fmt.Sprintf("%s", finallyBidding.contract),
				DoubleString:   fmt.Sprintf("%s", finallyBidding.dbType),
			}

			slog.Debug("GamePrivateNotyBid[競叫完畢]",
				slog.String(fmt.Sprintf("莊:%s  夢:%s  引:%s", CbSeat(declarer), CbSeat(dummy), CbSeat(lead)),
					fmt.Sprintf("花色: %s   合約: %s   賭倍: %s ", CbSuit(suit), finallyBidding.contract, finallyBidding.dbType),
				),
			)

			g.roomManager.SendPayloadsToPlayers(ClnRoomEvents.GameNotyFirstLead, payloadData{
				ProtoData:   &contractPayload,
				PayloadType: ProtobufType,
			})
		}
	}
	return nil
}

/*
func (g *Game) GamePrivateNotyBid(currentBidder *RoomUser) error {
	if !g.engine.IsBidValue8Valid(currentBidder.Bid8) {
		return ErrBiddingInvalid
	}

	var (
		isContractDone bool
		isReBid        bool
	)

	switch isContractDone, isReBid = g.engine.IsLastBidOrReBid(currentBidder.Bid8); isContractDone {
	case false:
		//叫牌仍持續中
		currentPlayer, bidValueLimit, db, db2, _ := g.engine.GetNextBid(currentBidder.Zone8, currentBidder.Bid8, currentBidder.Zone8|currentBidder.Bid8)

		//移動環形,並校準座位
		nextPlayer := g.SeatShift(currentPlayer)
		g.setEnginePlayer(currentPlayer, nextPlayer)

		//第一個參數: 表示下一個開叫牌者 前端(Player,觀眾席)必須處理
		//第二個參數: 禁叫品項,因為是首叫所以禁止叫品是 重要 zeroBid 前端(Player,觀眾席)必須處理
		//第三個參數: 上一個叫牌者
		//第四個參數: 上一次叫品
		g.roomManager.sendBytesToPlayers(append([]uint8{}, currentPlayer, bidValueLimit, currentBidder.Zone8, currentBidder.Bid8, db, db2), ClnRoomEvents.GamePrivateNotyBid)
		//TODO 廣播觀眾未實作
	case true:
		//合約底定
		if !isReBid {
			leadPlayer, declarer, dummy, contractSuit, rcd, err := g.engine.GameStartPlayInfo(currentBidder.Zone8)
			if err != nil {
				if errors.Is(err, ErrUnContract) {
					slog.Error("GamePrivateNotyBid", slog.String("FYI", fmt.Sprintf("合約有問題,只能在合約確定才能呼叫GameStartPlayInfo,%s", utilog.Err(err))))
					return err
				}
			}

			g.engine.ClearBiddingState()

			//TODO 未來 工作
			//以首引生成 RoundSuit keep
			//g.roundSuitKeeper = NewRoundSuitKeep(leadPlayer)

			nextPlayer := g.SeatShift(leadPlayer)
			g.setEnginePlayer(leadPlayer, nextPlayer)

			//送出首引封包
			// 封包位元依序為:首引, 莊家, 夢家, 合約王牌,王牌字串, 合約線位, 線位字串
			contractPayload := cb.Contract{
				Lead:           uint32(leadPlayer),
				Declarer:       uint32(declarer),
				Dummy:          uint32(dummy),
				Suit:           uint32(contractSuit),
				Contract:       uint32(rcd.contract),
				SuitString:     fmt.Sprintf("%s", CbSuit(contractSuit)),
				ContractString: fmt.Sprintf("%s", rcd.contract),
				DoubleString:   fmt.Sprintf("%s", rcd.dbType),
			}

			g.roomManager.SendPayloadsToPlayers(ClnRoomEvents.GameNotyFirstLead, payloadData{
				ProtoData:   &contractPayload,
				PayloadType: ProtobufType,
			})

			//TODO 廣播觀眾未實作

		} else {

			//送出第一次中發牌封包,前端 清空,重新設定BidTable
			g.roomManager.sendBytesToPlayers(append([]uint8{}, valueNotSet, valueNotSet, valueNotSet, valueNotSet),
				ClnRoomEvents.GamePrivateNotyBid)

			//清除叫牌紀錄
			g.engine.ClearBiddingState()

			//現出另三家的底牌,三秒後在重新發新牌
			g.roomManager.SendPlayersHandDeal()
			time.Sleep(time.Second * 3)

			// StartOpenBid會更換新一局,因此玩家順序也做了更動
			bidder, zero := g.start()

			g.roomManager.SendDeal()

			g.roomManager.sendBytesToPlayers(append([]uint8{}, bidder, zero, valueNotSet, valueNotSet),
				ClnRoomEvents.GamePrivateNotyBid)

		}
	}
	return nil
} */

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
