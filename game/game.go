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
	//"google.golang.org/protobuf/proto"
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

		// 當前的莊家, 夢家, 首引, 防家, 競叫玩遊戲開始前SetGamePlayInfo會設定這些值
		Declarer CbSeat
		Dummy    CbSeat
		Lead     CbSeat
		Defender CbSeat
		KingSuit CbSuit // 當前的王牌

		//首引產生以及每回合首打產生時會計算(SetRoundAvailableRange)該回合可出牌區間最大值,最小值
		roundMax uint8
		roundMin uint8
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

		roundMax: spadeAce,
		roundMin: club2,
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
	//重要: 只要Exception(panic)時看到下面這行出現,表示執行中的執行緒出錯
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

// 設定當前輪到哪一位玩家(座位),結算該回合比牌時會用到
func (g *Game) setEnginePlayer(player uint8) {
	// player 設定player當前玩家, 回合結束算牌時,需要知道current seat
	g.engine.SetCurrentSeat(player)
}

// SeatShift 移動到下一位玩家,以當前座位取得下一位玩家座位
func (g *Game) SeatShift(seat uint8) (nextSeat uint8) {
	return g.roomManager.SeatShift(seat)
}

// start 開始遊戲,這個method會進行洗牌, bidder競叫者,zeroBidding競叫初始值
func (g *Game) start() (currentPlayer, zeroBidding uint8) {
	//洗牌
	Shuffle(g)

	// limitBiddingValue 必定是 zeroBid ,因此 重要 前端必須判斷開叫是否是首叫狀態
	currentPlayer, zeroBidding = g.engine.StartBid()
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

//====================================================================================
//====================================================================================
//====================================================================================

// SetGamePlayInfo 競叫合約成立時,或遊戲重新開始時設定 Game,以及Engine中的Declarer, Dummy, Lead, KingSuit
func (g *Game) SetGamePlayInfo(declarer, dummy, firstLead, kingSuit uint8) {
	g.KingSuit = CbSuit(kingSuit)

	//TODO 設定Engine trumpRange
	g.engine.trumpRange = GetTrumpRange(kingSuit)

	switch g.KingSuit {
	case ZeroSuit: /*清除設定*/
		g.Declarer = seatYet
		g.Dummy = seatYet
		g.Lead = seatYet
		g.engine.declarer = seatYet
		g.engine.dummy = seatYet
	default: /*設定*/
		g.Declarer = CbSeat(declarer)
		g.Dummy = CbSeat(dummy)
		g.Lead = CbSeat(firstLead)
		g.engine.declarer = g.Declarer
		g.engine.dummy = g.Dummy
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

	//叫牌開始,開始設定這局Engine位置
	g.setEnginePlayer(currentBidder.Zone8)

	//移動環形,並校準座位
	next := g.SeatShift(currentBidder.Zone8)

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
			// moszorn 重要: 一並清除 bidHistory
			g.engine.ClearBiddingState()

			//四家攤牌
			g.roomManager.SendShowPlayersCardsOut()

			//三秒後重新發新牌
			time.Sleep(time.Second * 3)

			// StartOpenBid會更換新一局,因此玩家順序也做了更動
			bidder, zeroBidding := g.start()

			/* TBC: 因為產生新的玩家順序,所以要新的位置設定?? 但似乎好像這裡又不需要設定
			g.setEnginePlayer(bidder)

			//移動環形,並校準座位
			next := g.SeatShift(bidder)
			*/

			//重發牌
			g.roomManager.SendDeal()

			/*TODO 修改:
			1)送出Public (GameNotyBid)
			2)送出Private (GamePrivateNotyBid)..................................................
			*/

			notyBid := cb.NotyBid{
				Bidder:     uint32(bidder),
				BidStart:   uint32(zeroBidding), /*前端重新叫訊號*/
				LastBidder: uint32(currentBidder.Zone8),
				//LastBidderName: fmt.Sprintf("%s-%s", CbSeat(currentBidder.Zone8), currentBidder.Name),
				//LastBid:        fmt.Sprintf("%s", CbBid(currentBidder.Bid8)),
				Double1: uint32(db1.value),
				Double2: uint32(db2.value),
				Btn:     cb.NotyBid_disable_all,
			}
			payload.ProtoData = &notyBid

			//Public廣播
			if err := g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameNotyBid, payload, pb.SceneType_game); err != nil {
				//TODO 清空當前該遊戲桌在Server上的狀態
				slog.Info("GamePrivateNotyBid[重新洗牌,重新競叫]", utilog.Err(err))
				g.engine.ClearBiddingState()
			}

			time.Sleep(time.Millisecond * 400)
			//Private 指定傳送給 bidder 開叫
			payload.Player = bidder
			g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateNotyBid, payload) //私人Private

		case false: //競叫完成,遊戲開始

			lead, declarer, dummy, suit, finallyBidding, err := g.engine.GameStartPlayInfo()

			g.SetGamePlayInfo(declarer, dummy, lead, suit)

			if err != nil {
				if errors.Is(err, ErrUnContract) {
					slog.Error("GamePrivateNotyBid[競叫完成,遊戲開始]", slog.String("FYI", fmt.Sprintf("合約有問題,只能在合約確定才能呼叫GameStartPlayInfo,%s", utilog.Err(err))))
					//TODO 紀錄 log
					return
				}
			}

			g.engine.ClearBiddingState()

			// 向前端發送清除Bidding UI, 並停止(terminate)四家gauge
			var clearScene = pb.OP{
				Type:     pb.SceneType_game_clear_scene,
				RealSeat: uint32(currentBidder.Zone8),
			}
			payload.ProtoData = &clearScene
			g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameOP, payload, pb.SceneType_game)

			//TODO 未來 工作
			//todo zorn 這裡記住 RoundSuitKeep, 也是第一次紀錄RoundSuitKeep的地方
			//以首引生成 RoundSuit keep
			//g.roundSuitKeeper = NewRoundSuitKeep(lead)

			/* TBC: 因為產生新的玩家順序,所以要新的位置設定
			g.setEnginePlayer(currentBidder.Zone8)

			//移動環形,並校準座位
			next := g.SeatShift(currentBidder.Zone8)
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

			slog.Debug("GamePrivateNotyBid[競叫完成,遊戲開始]",
				slog.String(fmt.Sprintf("莊:%s  夢:%s  引:%s", CbSeat(declarer), CbSeat(dummy), CbSeat(lead)),
					fmt.Sprintf("花色: %s   合約: %s   賭倍: %s ", CbSuit(suit), finallyBidding.contract, finallyBidding.dbType),
				),
			)
			//TODO 廣播給三家,但是不要送給首引
			//    底下廣播給四家包含首引,目前workaround是前端gameFirstLead擋掉當首引是Global.loginUser.zone則跳掉gauge, 因為首引的gauge必須於底下 gamePrivateFirstLead 觸發
			payload.ProtoData = &firstLead
			//原來 g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameFirstLead, payload, pb.SceneType_game)
			g.roomManager.SendPayloadTo3Players(ClnRoomEvents.GameFirstLead, payload, lead)

			// TODO 新增一個 SendPayloadTo3Players(eventName, payload, exclude uint8)

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

			// 通知首引準備出牌 開啟 首引 card enable, 告知首引可打的牌與timeout, 觸發 gauge
			leadNotice := new(cb.PlayNotice)
			leadNotice.Seat = uint32(lead)
			leadNotice.CardMinValue, leadNotice.CardMaxValue, leadNotice.TimeoutCardValue, leadNotice.TimeoutCardIdx = g.AvailablePlayRange(lead)
			payload.ProtoData = leadNotice
			payload.Player = lead                                                          //傳給首引玩家                                                      //指定傳送給 bidder 開叫
			g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateFirstLead, payload) //私人Private
		}
	}
}

// GamePrivateFirstLead 打出首引
/*
	memo 回覆:
     (0) 首引座位打出的牌 (0.1)首引座位 (0.2) 停止首引座位Gauge; (0.3)前端開始下一家倒數 (0.4) 首引座位打出後,首引座位的牌組回給首引做UI牌重整
	 (1) 廣播亮出夢家牌組 (1.1)夢家座位
	 (2) 通知下一位出牌者 (2.1)下一位出牌者可打出的牌, (2.2)下一位若過了指定時間(gauge),自動打出哪張牌

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
	slog.Debug("FYI", slog.String("Declarer", fmt.Sprintf("%s", CbSeat(uint8(g.Declarer)))), slog.String("Dummy", fmt.Sprintf("%s", CbSeat(uint8(g.Dummy)))), slog.String("Lead", fmt.Sprintf("%s", CbSeat(uint8(g.Lead)))), slog.String("Defender", fmt.Sprintf("%s", CbSeat(uint8(g.Defender)))))
	slog.Debug("首引打出", slog.String("FYI", fmt.Sprintf("首引%s 打出 %s", CbSeat(leadPlayer.Zone8), CbCard(leadPlayer.Play8))))

	// memo 1)向三家亮夢家牌 TODO: 太難看了, Refactor 一包送三家
	//g.roomManager.SendPayloadToSeats(ClnRoomEvents.GameCardAction, payloadAttack, exclude)

	g.roomManager.SendPayloadsToPlayers(ClnRoomEvents.GamePrivateShowHandToSeat,
		payloadData{
			ProtoData: &cb.PlayersCards{
				Seat: uint32(g.Dummy), /*亮夢家牌*/
				Data: map[uint32][]uint8{
					uint32(g.Defender): g.deckInPlay[uint8(g.Dummy)][:], /*向防家亮夢家*/
				},
			},
			PayloadType: ProtobufType,
			Player:      uint8(g.Defender), /*坑:忘了加上Player,所以之直往東送*/
		},
		payloadData{
			ProtoData: &cb.PlayersCards{
				Seat: uint32(g.Dummy), /*亮夢家牌*/
				Data: map[uint32][]uint8{
					uint32(g.Lead): g.deckInPlay[uint8(g.Dummy)][:], /*向首引(防家)亮夢家*/
				},
			},
			PayloadType: ProtobufType,
			Player:      uint8(g.Lead),
		},
		payloadData{
			ProtoData: &cb.PlayersCards{
				Seat: uint32(g.Dummy), /*亮夢家牌*/
				Data: map[uint32][]uint8{
					uint32(g.Declarer): g.deckInPlay[uint8(g.Dummy)][:], /*向莊家亮夢家*/
				},
			},
			PayloadType: ProtobufType,
			Player:      uint8(g.Declarer),
		},
	)

	// memo 0) 向三家亮出首引出的牌 CardAction, 首引的GameAction.IsCardCover要是false,且要包含refresh
	// memo (0)首引座位打出的牌
	//      (0.1) 首引座位
	//      (0.2) 停止首引座位Gauge
	//      (0.2) 前端開始下一家倒數(gauge)
	//      (0.4) 首引為特殊CardAction,首引座位打出後,首引座位的牌組回給首引做UI牌重整
	var (
		nextPlayer = g.SeatShift(leadPlayer.Zone8)

		refresh, outCardIdx = g.PlayOutHandRefresh(leadPlayer.Zone8, leadPlayer.Play8)

		coverCardAction = &cb.CardAction{
			Type:        cb.CardAction_play,
			CardValue:   leadPlayer.Play,
			Seat:        leadPlayer.Zone,    /*停止的Gauge*/
			NextSeat:    uint32(nextPlayer), /*下一家Gauge*/
			IsCardCover: true,               /*蓋牌打出*/
		}
		faceCardAction = &cb.CardAction{
			AfterPlayCards: refresh, /*出牌後首引重整牌組*/
			Type:           cb.CardAction_play,
			CardValue:      leadPlayer.Play,
			Seat:           leadPlayer.Zone,    /*停止的Gauge*/
			NextSeat:       uint32(nextPlayer), /*下一家Gauge*/
			IsCardCover:    false,              /*明牌打出*/
			CardIndex:      outCardIdx,         /*明牌打出要加上索引,前端好處理*/
		}
		//三家UI收到蓋牌出牌
		commonPayload = payloadData{
			ProtoData:   coverCardAction,
			PayloadType: ProtobufType,
		}
		//首引UI收到自己的出牌,以及refresh hand
		specialPayload = payloadData{
			ProtoData:   faceCardAction,
			PayloadType: ProtobufType,
		}
	)
	g.roomManager.SendPayloadToOneAndPayloadToOthers(ClnRoomEvents.GameCardAction,
		commonPayload,
		specialPayload,
		leadPlayer.Zone8)

	// TODO: 尚未完成, 需要新的 Proto定義
	// memo 2) 通知下家換誰出牌  (注意:首引後的出牌者是莊家要打夢家的牌)
	//    (2.0)下一位出牌者 (莊)
	//    (2.1)下一位出牌者可打出的牌 range (Max,min)
	//    (2.2)下一位若過了指定時間(gauge),自動打出哪張牌 (必定在range間,否則索引第一張)
	g.SetRoundAvailableRange(leadPlayer.Play8) //回合首打制定回合出牌範圍
	var (
		nextRealPlaySeat = g.playTurn(nextPlayer)

		nextNotice = &cb.PlayNotice{
			Type:         cb.PlayNotice_Turn,
			IsPlayAgent:  true, /*若為莊打夢,則前端要修正seat為封包發送者(nextRealPlaySeat)*/
			Seat:         uint32(nextPlayer),
			PreviousSeat: leadPlayer.PlaySeat, /*為了停止上一次的gauge*/
		}
		payload = payloadData{
			Player:      nextRealPlaySeat, //封包送下一個玩家(首引後的玩家是莊打夢)
			PayloadType: ProtobufType,
		}
	)
	slog.Debug("下一個玩家更替",
		slog.String("FYI", fmt.Sprintf("莊:%s 夢:%s", g.Declarer, g.Dummy)),
		slog.String("原本", fmt.Sprintf("%s", CbSeat(nextPlayer))),
		slog.String("實際", fmt.Sprintf("%s", CbSeat(nextRealPlaySeat))),
	)
	//莊家打夢家,所以要找出夢家可出牌範圍
	nextNotice.CardMinValue, nextNotice.CardMaxValue, nextNotice.TimeoutCardValue, nextNotice.TimeoutCardIdx = g.AvailablePlayRange(nextPlayer)
	payload.ProtoData = nextNotice
	g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateCardPlayClick, payload) //私人Private

	return nil
}

// 傳入下一位玩家座位,回傳實際出牌的玩家(例如: 下一位是夢家,但實際出牌的是莊家)
func (g *Game) playTurn(nextPlayer uint8) uint8 {
	nextSeat := CbSeat(nextPlayer)

	switch nextSeat {
	case g.Dummy:
		return uint8(g.Declarer)
	default:
		return nextPlayer
	}
}

// SetRoundAvailableRange 設定回合可出牌範圍(roundMin, roundMax)
func (g *Game) SetRoundAvailableRange(firstPlay uint8) {
	roundRange := GetRoundRangeByFirstPlay(firstPlay)
	g.roundMin = roundRange[0]
	g.roundMax = roundRange[1]
}

// AvailablePlayRange 玩家可出牌範圍最大值,最小值,依照 roundMin, roundMax決定
func (g *Game) AvailablePlayRange(player uint8) (minimum, maximum, timeout, timeoutCardIndex uint32) {
	var (
		hitAvailable = false //
		hitFirst     = false
		hand         = g.deckInPlay[player]

		//為了要讓底下if判斷式成立,所以將 m, M 分別設定到極限
		m, M = spadeAce + uint8(1), uint8(BaseCover)
	)
	//預設隨便出都可 (這表示,沒有可出的花色,可以任意出)
	minimum, maximum = uint32(club2), uint32(spadeAce)
	fmt.Printf("min: %s  (%d) ~  %s  (%d) \n", CbCard(g.roundMin), g.roundMin, CbCard(g.roundMax), g.roundMax)

	for i := range hand {
		if hand[i] == uint8(BaseCover) {
			continue
		}

		fmt.Printf(" %s   %d\n", CbCard(hand[i]), hand[i])
		// 陣列中第一張有效牌,與其索引
		if !hitFirst {
			hitFirst = true
			//先設定,若time gauge 時間到時,要出的牌 (一定是陣列中第一張有效牌)
			timeout = uint32(hand[i])
			timeoutCardIndex = uint32(i)
		}

		if g.roundMin <= hand[i] && g.roundMax >= hand[i] {
			//發現 player 手頭上有牌
			hitAvailable = true
			if M < hand[i] {
				M = hand[i]
				//fmt.Printf(" Max %d\n", M)
			}
			if m > hand[i] {
				m = hand[i]
				//fmt.Printf(" min: %d\n", m)
				timeoutCardIndex = uint32(i)
			}
		}
	}
	//手頭上有牌,則限定可出範圍最大值與最小值
	if hitAvailable {
		minimum = uint32(m)
		maximum = uint32(M)
		timeout = minimum
	}

	//minimum, maximum, timeout, timeoutCardIndex, player

	slog.Debug(fmt.Sprintf("%s可出牌區間", CbSeat(player)),
		slog.String("FYI", fmt.Sprintf("%s  ~  %s   timeout: %s (索引值:%d)", CbCard(minimum), CbCard(maximum), CbCard(timeout), timeoutCardIndex)))

	return
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

// GamePrivateCardPlayClick 玩家打出牌
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
func (g *Game) GamePrivateCardPlayClick(clickPlayer *RoomUser) error {

	slog.Debug("出牌", slog.String("FYI",
		fmt.Sprintf("%s (%s) 打出 %s 牌 %s , (%s)的牌被打出",
			CbSeat(clickPlayer.Zone8),
			clickPlayer.Name,
			CbSeat(clickPlayer.PlaySeat),
			CbCard(clickPlayer.Play8),
			CbSeat(clickPlayer.PlaySeat8),
		)))

	var (
		nextPlayer       = g.SeatShift(clickPlayer.PlaySeat8)
		nextRealPlaySeat = g.playTurn(nextPlayer)

		refresh, outCardIdx = g.PlayOutHandRefresh(clickPlayer.PlaySeat8, clickPlayer.Play8)

		attackCardAction = &cb.CardAction{
			AfterPlayCards: nil, /*後面決定*/
			Type:           cb.CardAction_play,
			CardIndex:      10000, /*後面決定*/
			CardValue:      uint32(clickPlayer.Play8),
			Seat:           uint32(clickPlayer.PlaySeat8),
			NextSeat:       uint32(nextRealPlaySeat),
			IsCardCover:    false, /*後面決定*/
		}
		defenderCardAction = &cb.CardAction{
			Type:        attackCardAction.Type,
			CardIndex:   attackCardAction.CardIndex, /*後面決定*/
			CardValue:   attackCardAction.CardValue,
			Seat:        attackCardAction.Seat,
			NextSeat:    attackCardAction.NextSeat,
			IsCardCover: true, /*後面決定*/
		}

		nextNotice = &cb.PlayNotice{
			Type:         cb.PlayNotice_Turn,
			PreviousSeat: attackCardAction.Seat,
			Seat:         attackCardAction.NextSeat,
			IsPlayAgent:  nextPlayer != nextRealPlaySeat,
		}
		payloadAttack = payloadData{
			ProtoData:   attackCardAction,
			PayloadType: ProtobufType,
		}
		payloadDefender = payloadData{
			ProtoData:   defenderCardAction,
			PayloadType: ProtobufType,
		}
		noticePayload = payloadData{
			ProtoData:   nextNotice,
			PayloadType: ProtobufType,
		}
	)

	// 重要: 判斷誰打出的牌,可透過 RoomUser PlaySeat8 屬性
	switch clickPlayer.PlaySeat8 {
	case clickPlayer.Zone8: //莊打莊,防打防 *

		switch clickPlayer.Zone8 {
		case uint8(g.Declarer): //莊打出
			//			▶️ UI - 莊家,與夢家會看到直接打出的明牌, 莊家,與夢家會看到莊家手上重整後的牌組
			//			▶️ UI - 防家會看到莊打出暗牌翻明牌
			//1. 設定 夢家的 refresh
			//attackCardAction.AfterPlayCards = refresh
			//attackCardAction.CardIndex = outCardIdx //TBC 似乎可以省略
			//attackCardAction.IsCardCover = false

			//2. 防家會看到夢的暗牌翻明
			defenderCardAction.CardIndex = outCardIdx
			defenderCardAction.IsCardCover = true

			g.roomManager.SendPayloadToDefendersToAttacker(ClnRoomEvents.GameCardAction, payloadDefender, payloadAttack, uint8(g.Declarer), uint8(g.Dummy))

		default: //防打出
			//			▶️ UI - 莊家,與夢家與防家夥伴會看到打出的暗牌變明牌
			//			▶️ UI - 該防家會看到自己打出明牌, 和該防家手上重的整牌

			attackCardAction.AfterPlayCards = nil
			attackCardAction.CardIndex = outCardIdx //TBC 似乎可以省略
			attackCardAction.IsCardCover = true

			//TODO: 莊,夢,防家夥伴一包送三個
			//exclude := clickPlayer.Zone8
			//g.roomManager.SendPayloadToSeats(ClnRoomEvents.GameCardAction, payloadAttack, exclude)

			//TODO: 防家要分兩個封包,因為打出者要現名牌,另一個防家要現暗牌
			//1. 打出者防家
			defenderCardAction.IsCardCover = false
			defenderCardAction.AfterPlayCards = refresh
			defenderCardAction.CardIndex = outCardIdx                                        //TBC 似乎可以省略
			g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GameCardAction, payloadDefender) //私人Private

		} //eofSwitch

	default: //莊打夢
		if uint8(g.Declarer) != clickPlayer.Zone8 {
			slog.Warn("莊家身份錯誤", slog.String("FYI", fmt.Sprintf("莊應為:%s ,但收到 %s 打出 %s", g.Declarer, CbSeat(clickPlayer.Zone8), CbCard(clickPlayer.Play8))))
			return nil
		}
		if uint8(g.Dummy) != clickPlayer.PlaySeat8 {
			slog.Warn("夢家身份錯誤", slog.String("FYI", fmt.Sprintf("夢應為:%s ,但收到 %s 打出 %s", g.Dummy, CbSeat(clickPlayer.PlaySeat8), CbCard(clickPlayer.Play8))))
			return nil
		}

		//1. 設定 夢家的 refresh
		//attackCardAction.AfterPlayCards = refresh
		//attackCardAction.CardIndex = outCardIdx //TBC 似乎可以省略
		//attackCardAction.IsCardCover = false

		//2. 防家會看到夢的暗牌翻明
		defenderCardAction.CardIndex = outCardIdx
		defenderCardAction.IsCardCover = true

		g.roomManager.SendPayloadToDefendersToAttacker(ClnRoomEvents.GameCardAction, payloadDefender, payloadAttack, uint8(g.Declarer), uint8(g.Dummy))
	}

	//設定下一位玩家通知
	nextNotice.CardMinValue, nextNotice.CardMaxValue, nextNotice.TimeoutCardValue, nextNotice.TimeoutCardIdx = g.AvailablePlayRange(nextPlayer)
	g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateCardPlayClick, noticePayload) //私人Private
	return nil
}

// PlayOutHandRefresh 打出牌後,修改手頭上剩下的牌組,並回傳修正後的clone牌組給前端進行牌重整,以及打出這張牌在牌組中的索引.
// player8 出牌的座位, card8 出的牌
func (g *Game) PlayOutHandRefresh(player8, card8 uint8) (refresh []uint8, cardIdx uint32) {
	var (
		cards       = g.deckInPlay[player8][:]
		cardsLength = uint32(len(cards))
		cardCover   = uint8(BaseCover)
	)

	//減1的原因是refresh是收集有效牌,已經打出去的不算,下面的 cards[cardIdx]=cardCover就會被濾掉
	refresh = make([]uint8, 0, cardsLength-1)

	//找出打出的那張牌的索引設定為 BaseCover, 並收集下一次可打的牌
	for ; cardIdx < cardsLength; cardIdx++ {
		if cards[cardIdx] != card8 && cards[cardIdx] != cardCover {
			refresh = append(refresh, cards[cardIdx])
			continue
		}
		cards[cardIdx] = cardCover // 0
	}
	return refresh, cardIdx
}

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
