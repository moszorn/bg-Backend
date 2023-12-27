package game

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/moszorn/pb"
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

type (
	PayloadType uint8
)

const (
	ByteType PayloadType = iota
	ProtobufType
)

type (
	payloadData struct {
		Player      uint8         //代表player seat 通常針對指定的玩家, 表示Zone的情境應該不會發生
		Data        []uint8       // 可以是byte, bytes
		ProtoData   proto.Message // proto
		PayloadType PayloadType   //這個 payload 屬於那種型態的封	包
	}

	Game struct { // 玩家進入房間, 玩家進入遊戲,玩家離開房間,玩家離開遊戲

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

		// 當前的莊家, 夢家, 首引, 防家
		Declarer CbSeat
		Dummy    CbSeat
		Lead     CbSeat
		Defender CbSeat

		// TODO 當前的王牌
		KingSuit CbSuit
	}
)

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
	//重要: 只要Exception(panic)時看到下面這行出現,表示執行緒中出錯
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

// SetStartPlayInfo 競叫合約成立時,或遊戲重新開始時設定 Game中的Declarer, Dummy, Lead, KingSuit
func (g *Game) SetStartPlayInfo(declarer, dummy, firstLead, kingSuit uint8) {
	g.KingSuit = CbSuit(kingSuit)
	switch g.KingSuit {
	case ZeroSuit: /*清除設定*/
		g.Declarer = seatYet
		g.Dummy = seatYet
		g.Lead = seatYet
	default: /*設定*/
		g.Declarer = CbSeat(declarer)
		g.Dummy = CbSeat(dummy)
		g.Lead = CbSeat(firstLead)
	}

	//找出首引對家 (防家)
	switch g.Lead {
	case east:
		g.Defender = west
	case south:
		g.Defender = north
	case west:
		g.Defender = east
	case north:
		g.Defender = south
	default:
		g.Defender = seatYet
	}
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
func (g *Game) GamePrivateNotyBid(currentBidder *RoomUser) {

	nextLimitBidding, db1, db2 := g.engine.GetNextBid(currentBidder.Zone8, currentBidder.Bid8)

	g.setEnginePlayer(currentBidder.Zone8)

	//移動環形,並校準座位
	next := g.SeatShift(currentBidder.Zone8)

	/* Old
	   //設定Engine當前玩家與下一個玩家

	   	//step1. 設定位置環形
	   	//nextPlayer := g.SeatShift(currentPlayer)

	   	//設定引擎
	   	//g.setEnginePlayer(nextPlayer)

	*/

	complete, needReBid := g.engine.IsBidFinishedOrReBid()

	var payload = payloadData{PayloadType: ProtobufType}

	switch complete {
	case false: //仍在競叫中
		//第一個參數: 表示下一個開叫牌者 前端(Player,觀眾席)必須處理
		//第二個參數: 禁叫品項,因為是首叫所以禁止叫品是 重要 zeroBid 前端(Player,觀眾席)必須處理
		//第三個參數: 上一個叫牌者
		//第四個參數: 上一次叫品

		notyBid := cb.NotyBid{
			Bidder:         uint32(next),
			BidStart:       uint32(nextLimitBidding),
			LastBidder:     uint32(currentBidder.Zone8),
			LastBidderName: fmt.Sprintf("%s-%s", CbSeat(currentBidder.Zone8), currentBidder.Name),
			LastBid:        fmt.Sprintf("%s", CbBid(currentBidder.Bid8)),
			Double1:        uint32(db1.value),
			Double2:        uint32(db2.value),
			Btn:            0,
		}

		switch true {
		case db1.isOn:
			notyBid.Btn = cb.NotyBid_db
		case db2.isOn:
			notyBid.Btn = cb.NotyBid_dbx2
		default:
			notyBid.Btn = cb.NotyBid_disable_all
		}

		payload.ProtoData = &notyBid

		/*TODO 修改:
		1)送出Public (GameNotyBid)
		2)送出Private (GamePrivateNotyBid)..................................................
		 memo TODO 當出現有人斷線
		   要廣播清空桌面資訊,並告知有人斷線

		 TODO: 另一種狀況是,玩家離開遊戲桌,也必須告知前端有人離桌,並清空桌面,
		*/
		g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameNotyBid, payload, pb.SceneType_game) //廣播Public
		time.Sleep(time.Millisecond * 400)

		payload.Player = next                                                        //指定傳送給 bidder 開叫
		g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateNotyBid, payload) //私人Private

	case true: //競叫完成
		switch needReBid {
		case true: //重新洗牌,重新競叫

			//清除叫牌紀錄
			g.engine.ClearBiddingState()

			//現出另三家的底牌,三秒後在重新發新牌 TODO: 改名 SendPlayersHandDeal名子很白癡
			g.roomManager.SendPlayersHandDeal()
			time.Sleep(time.Second * 3)

			// StartOpenBid會更換新一局,因此玩家順序也做了更動
			bidder, _ := g.start()

			//前端重新叫訊號
			reBidSignal := valueNotSet

			//重發牌
			g.roomManager.SendDeal()

			/*TODO 修改:
			1)送出Public (GameNotyBid)
			2)送出Private (GamePrivateNotyBid)..................................................
			*/

			notyBid := cb.NotyBid{
				Bidder:     uint32(bidder),
				BidStart:   uint32(reBidSignal),
				LastBidder: uint32(currentBidder.Zone8),
				//LastBidderName: fmt.Sprintf("%s-%s", CbSeat(currentBidder.Zone8), currentBidder.Name),
				//LastBid:        fmt.Sprintf("%s", CbBid(currentBidder.Bid8)),
				Double1: uint32(db1.value),
				Double2: uint32(db2.value),
				Btn:     cb.NotyBid_disable_all,
			}
			payload.ProtoData = &notyBid
			/*
				g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameNotyBid, payload, BidScene) //廣播Public
				time.Sleep(time.Millisecond * 400)

				payload.Player = bidder                                                      //指定傳送給 bidder 開叫
				g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateNotyBid, payload) //私人Private

			*/

			if err := g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameNotyBid, payload, pb.SceneType_game); err != nil { //廣播Public
				//TODO 清空當前該遊戲桌在Server上的狀態
				slog.Info("GamePrivateNotyBid", utilog.Err(err))
				g.engine.ClearBiddingState()
			}
			time.Sleep(time.Millisecond * 400)

			payload.Player = bidder                                                      //指定傳送給 bidder 開叫
			g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateNotyBid, payload) //私人Private
		case false: //競叫完成,遊戲開始

			lead, declarer, dummy, suit, finallyBidding, err := g.engine.GameStartPlayInfo()

			g.SetStartPlayInfo(declarer, dummy, lead, suit)

			if err != nil {
				if errors.Is(err, ErrUnContract) {
					slog.Error("GamePrivateNotyBid", slog.String("FYI", fmt.Sprintf("合約有問題,只能在合約確定才能呼叫GameStartPlayInfo,%s", utilog.Err(err))))
					//TODO 紀錄 log
					return
				}
			}

			g.engine.ClearBiddingState()

			// 向前端發送清除Bidding UI
			var clearScene = pb.OP{
				Type:     pb.SceneType_game_clear_scene,
				RealSeat: uint32(currentBidder.Zone8),
			}
			payload.ProtoData = &clearScene
			g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameOP, payload, pb.SceneType_game)

			//TODO 未來 工作
			//todo zorn 這裡記住 RoundSuitKeep, 也是第一次紀錄RoundSuitKeep的地方
			//以首引生成 RoundSuit keep
			g.roundSuitKeeper = NewRoundSuitKeep(lead)

			//nextPlayer := g.SeatShift(leadPlayer)
			//g.setEnginePlayer(leadPlayer, nextPlayer)

			/* memo
			   ............................................
			*/

			//送出首引封包
			// 封包位元依序為:首引, 莊家, 夢家, 合約王牌,王牌字串, 合約線位, 線位字串
			firstLead := cb.Contract{
				LastBidder:     uint32(currentBidder.Zone8),
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
			//廣播
			payload.ProtoData = &firstLead
			g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameFirstLead, payload, pb.SceneType_game)

			// 重要: g.syncPlayCard 很重要
			//TODO: 將莊家牌組發送給夢家
			// toDummy := g.deckInPlay[declarer][:] //取得莊家牌
			payload.ProtoData = &cb.PlayersCards{
				Seat: uint32(declarer), /*亮莊家牌*/
				Data: map[uint32][]uint8{
					uint32(dummy): g.deckInPlay[declarer][:], /*向夢家亮莊家牌*/
				},
			}
			//向夢家亮莊家的牌
			payload.Player = dummy
			g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateShowHandToSeat /*向夢家亮莊家的牌*/, payload) //私人Private

			time.Sleep(time.Millisecond * 400)
			//TODO 通知首引準備出牌 開啟 首引 card enable
			payload.Player = lead //傳給首引玩家                                                      //指定傳送給 bidder 開叫
			payload.ProtoData = &firstLead
			g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateFirstLead, payload) //私人Private
		}
	}
}

// GamePrivateFirstLead 打出首引
/*
	memo 回覆:
     (0) 首引座位打出的牌 (0.1)首引座位 (0.2) 停止首引座位Gauge; (0.3)前端開始下一家倒數
	 (1) 廣播亮出夢家牌組 (1.1)夢家座位
	 (2) 通知下一位出牌者 (2.1)下一位出牌者可打出的牌, (2.2)下一位若過了指定時間(gauge),自動打出哪張牌
	 (3) 首引座位打出後,首引座位的牌組回給首引做UI牌重整

	如何判斷gauge 時間終了要打出哪張牌
    想法:
		1) 先看此輪首打花色,然後在 deckInPlay尋找到第一張與首打花色一樣花色的牌,它就是接著要跟的牌
		2) 若找不到,則從deckInPlay第一張打出
*/
func (g *Game) GamePrivateFirstLead(leadPlayer *RoomUser) error {

	if leadPlayer.Zone8 != uint8(g.Lead) {
		slog.Warn("首引出牌", slog.String("FYI", fmt.Sprintf("首引應為%s, 但引牌方為%s", g.Lead, CbSeat(leadPlayer.Zone8))))
		return nil //by pass
	}

	slog.Debug("首引打出", slog.String("FYI", fmt.Sprintf("首引%s 打出 %s", CbSeat(leadPlayer.Zone8), CbCard(leadPlayer.Play8))))

	// memo 1)向三家亮夢家牌
	g.roomManager.SendPayloadsToPlayers(ClnRoomEvents.GamePrivateShowHandToSeat,
		payloadData{
			ProtoData: &cb.PlayersCards{
				Seat: uint32(g.Dummy), /*亮夢家牌*/
				Data: map[uint32][]uint8{
					uint32(g.Declarer): g.deckInPlay[uint8(g.Dummy)][:], /*向莊家亮夢家*/
				},
			},
			PayloadType: ProtobufType,
		},
		payloadData{
			ProtoData: &cb.PlayersCards{
				Seat: uint32(g.Dummy), /*亮夢家牌*/
				Data: map[uint32][]uint8{
					uint32(g.Lead): g.deckInPlay[uint8(g.Dummy)][:], /*向首引(防家)亮夢家*/
				},
			},
			PayloadType: ProtobufType,
		},
		payloadData{
			ProtoData: &cb.PlayersCards{
				Seat: uint32(g.Dummy), /*亮夢家牌*/
				Data: map[uint32][]uint8{
					uint32(g.Defender): g.deckInPlay[uint8(g.Dummy)][:], /*向防家亮夢家*/
				},
			},
			PayloadType: ProtobufType,
		},
	)

	// memo 0) 向四家亮出首引出的牌 CardAction
	// memo (0)首引座位打出的牌
	//      (0.1) 首引座位
	//      (0.2) 停止首引座位Gauge
	//      (0.2) 前端開始下一家倒數(gauge)
	var (
		nextPlayer = g.SeatShift(leadPlayer.Zone8)
		cardAction = &cb.CardAction{
			Type:        cb.CardAction_play,
			CardValue:   leadPlayer.Play,
			Seat:        leadPlayer.Zone,
			NextSeat:    uint32(nextPlayer),
			IsCardCover: true, /*蓋牌打出*/
		}
		payload = payloadData{
			ProtoData:   cardAction,
			PayloadType: ProtobufType,
		}
	)
	g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameCardAction, payload, pb.SceneType_game)

	// TODO: 尚未完成
	// memo 2) 通知下家換誰出牌
	//    (2)下一位出牌者 (2.1)下一位出牌者可打出的牌, (2.2)下一位若過了指定時間(gauge),自動打出哪張牌
	//g.roomManager.SendPayloadToPlayer(ClnRoomEvents., payload) //私人Private

	// memo 3) 回覆重整首引座位手持牌組 注意: 重整牌一定會要設定以下三個參數
	cardAction.Seat = leadPlayer.Zone
	cardAction.IsCardCover = false                                           //重要,一定要false前端才會正確
	cardAction.AfterPlayCards = g.deckInPlay[leadPlayer.Zone8][:]            //牌重整
	g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GameCardAction, payload) //私人Private

	return nil
}

// GamePrivateCardHover hoverPlayer 可能是莊家,能是夢家 ->對應前端 GameCardAction
//
//		當莊家滑過牌(莊家,夢家)時,所有的hover/hover out 一併夢家也會看到莊家的動作
//	      🥎 ) 回覆當莊家對莊家自己的牌發生hover時
//			UI) 夢家會看到莊家的那張牌 hover
//
//		  🥎 ) 回覆當莊家對莊家自己的牌發生hover out時
//			UI) 夢家會看到莊家的那張牌 hover out
//
//	      🥎 ) 回覆當莊家對夢家的牌發生hover時
//			UI) 夢家會看到夢家的那張牌 hover
//
//
//		  🥎 ) 回覆當莊家對夢家的牌發生hover out時
//			UI) 夢家會看到夢家的那張牌 hover out
//
// hoverPlayer 一定是莊家(Declarer) memo : 已完成
func (g *Game) GamePrivateCardHover(cardAction *cb.CardAction) error {

	if !cardAction.IsTriggerByDeclarer {
		slog.Error("GamePrivateCardHover", utilog.Err(errors.New(fmt.Sprintf("觸發者應該是莊(%s)但觸發是 %s", g.Declarer, CbSeat(cardAction.Seat)))))
		return nil
	}

	if cardAction.Type == cb.CardAction_play {
		slog.Error("GamePrivateCardHover", utilog.Err(errors.New(fmt.Sprintf(" %s  型態應該是hover/out但傳入型態是Play", CbCard(cardAction.CardValue)))))
		return nil
	}
	//server trigger by pass 回前端夢家
	cardAction.IsTriggerByDeclarer = false

	//送出給Dummy
	g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateCardHover, payloadData{
		ProtoData:   cardAction,
		Player:      uint8(g.Dummy),
		PayloadType: ProtobufType,
	}) //私人Private

	return nil
}

// GamePrivateCardPlayClick 玩家打出牌, 必須回覆 pb.CardAction 讓前端的hand可以refresh
func (g *Game) GamePrivateCardPlayClick(clickPlayer *RoomUser) error {

	slog.Debug("出牌", slog.String("FYI",
		fmt.Sprintf("%s (%s) 打出 %s 牌 %s",
			CbSeat(clickPlayer.Zone8),
			clickPlayer.Name,
			CbSeat(clickPlayer.PlaySeat),
			CbCard(clickPlayer.Play8),
		)))

	// 重要: 判斷誰打出的牌,可透過 RoomUser PlaySeat8 屬性
	/*
		switch clickPlayer.PlaySeat8 {
		case clickPlayer.Zone8: //莊打莊,防打防 *
		case uint8(g.Dummy):
			if clickPlayer.Zone8 == uint8(g.Declarer) {
				//莊打夢
			}
		}
	*/

	/* 當玩家點擊出牌時,有底下情境與相應要處理的事情
	    當莊家點擊莊家牌時:
	      🥎 )回覆(四家UI)莊家打出什麼牌
	        ▶️ UI - 莊家,與夢家會看到直接打出的明牌, 莊家,與夢家會看到莊家手上重整後的牌組
			▶️ UI - 防家會看到莊打出暗牌翻明牌

	    當莊家點擊夢家牌時:
	      🥎 )回覆(四家UI)夢家打出什麼牌
	        ▶️ UI - 莊家,與夢家會看到直接打出的明牌, 莊家,與夢家會看到夢家家手上重整後的牌組
			▶️ UI - 防家會看到莊打出暗牌翻明牌

		當防家點擊防家牌時:
	      🥎 )回覆(四家UI)夢家打出什麼牌
	        ▶️ UI - 莊家,與夢家與防家夥伴會看到打出的暗牌變明牌
			▶️ UI - 該防家會看到自己打出明牌, 和該防家手上重的整牌

		🥎 )回覆打出的牌,一併回覆下一家Gauge PASS牌,與下一家限制可出的牌,並停止打出牌者的Gauge 停止OP
	*/

	var (
		payload payloadData = payloadData{
			PayloadType: ProtobufType,
		}

		//出牌者手上牌集合(hand)
		cards                        []uint8 = g.deckInPlay[clickPlayer.Zone8][:]
		cardValue, cardIdx, cardsLen uint32  = 0, 0, uint32(len(cards))

		//因為牌已經打出,所以向四家送出打出者Gauge的停止 OP
		op = &pb.OP{
			Type:     pb.SceneType_game_gauge_terminate, /*memo : SceneType_game_gauge_terminate 已經廢棄*/
			RealSeat: clickPlayer.Zone,
		}
	)

	//找出出牌者所出的牌在手上牌集合中對應的索引(cardIdx),與牌面值(cardValue)
	for ; cardIdx < cardsLen; cardIdx++ {
		if cards[cardIdx] == clickPlayer.Play8 {
			cardValue = uint32(cards[cardIdx]) //牌值
			break
		}
	}

	//重整私人hand
	//回覆給前端剛打出牌的玩家,進行整理畫面
	payload.Player = clickPlayer.Zone8
	payload.ProtoData = &cb.CardAction{
		Type:           cb.CardAction_play,
		CardIndex:      cardIdx,
		CardValue:      cardValue,
		Seat:           uint32(clickPlayer.Zone8), /*關閉Gauge,與執行該位置CardAction_play動作*/
		NextSeat:       uint32(clickPlayer.Zone8), /*啟動下一個Gauge*/
		IsCardCover:    false,                     /*重整牌一定要設成false*/
		AfterPlayCards: append(g.deckInPlay[clickPlayer.Zone8][:][:cardIdx], g.deckInPlay[clickPlayer.Zone8][:][cardIdx+1:]...),
	}
	g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GameCardAction, payload) //私人Private

	//廣播四家中斷gauge, memo: 使用OP停止Gauge方式已經廢棄,改用CardAction(Seat,NextSeat)
	payload.ProtoData = op
	g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameOP, payload, pb.SceneType_game)

	return nil
}

// TODO 打出牌後ㄝ剩下的牌組要回給前端莊家,與夢家進行牌重整

//
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
