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
		log      *utilog.MyLog
		Shutdown context.CancelFunc

		//計數入房間的人數,由UserCounter而設定
		CounterAdd roomUserCounter
		CounterSub roomUserCounter

		// 未來 當遊戲桌關閉時,記得一同關閉channel 以免leaking
		roomManager *RoomManager //管理遊戲房間所有連線(觀眾,玩家),與當前房間(Game)中的座位狀態
		engine      *Engine

		//roundSuitKeeper *RoundSuitKeep

		// Key: Ring裡的座位指標(SeatItem.Name), Value:牌指標
		// 並且同步每次出牌結果(依照是哪一家打出什牌並該手所打出的牌設成0指標
		Deck map[*uint8][]*uint8

		//遊戲中各家的持牌,會同步手上的出牌,打出的牌會設成0x0 CardCover
		deckInPlay map[uint8]*[NumOfCardsOnePlayer]uint8

		//代表遊戲中一副牌,從常數集合複製過來,參:dealer.NewDeck
		deck [NumOfCardsInDeck]*uint8

		//在_OnRoomJoined階段,透過 Game.userJoin 加入Users 觀眾
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

		eastCard  uint8
		southCard uint8
		westCard  uint8
		northCard uint8

		//首引產生以及每回合首打產生時會計算(SetRoundAvailableRange)該回合可出牌區間最大值,最小值
		roundMax uint8
		roundMin uint8
	}
)

// CreateCBGame 建立橋牌(Contract Bridge) Game
func CreateCBGame(log *utilog.MyLog, pid context.Context, counter UserCounter, tableName string, tableId int32) *Game {

	ctx, cancelFunc := context.WithCancel(pid)

	g := &Game{
		log:         log,
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

// start 開始遊戲,這個method會進行洗牌,並引擎記錄該局叫牌順序, bidder競叫者,zeroBidding競叫初始值
func (g *Game) start() (currentPlayer uint8) {
	//洗牌
	Shuffle(g)

	return g.engine.StartBid()
}

// GetBidOrder 執行GetBidOrder,必須是遊戲第一次開叫之後,也就是 engine的 StartBid已經被呼叫之後
func (g *Game) GetBidOrder() (order []uint32) {
	//從 array[4] 轉成 array
	return (*g.engine.bidOrder)[:]
}

func (g *Game) KickOutBrokenConnection(ns *skf.NSConn) {
	//清除叫牌紀錄
	// moszorn 重要: 一並清除 bidHistories
	//3-13 moszorn 重要 TODO: 底下會造成 bidhistory data racing , 參考 room_manager.go - PlayerLeave也有同樣的問題
	g.engine.ClearBiddingState()

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

func (g *Game) Chat(user *RoomUser) {
	g.roomManager.BroadcastProtobuf(user.NsConn, ClnRoomEvents.TableOnChat, g.name, user.Chat)
}

//====================================================================================
//====================================================================================
//====================================================================================

// SetGamePlayInfo 競叫合約成立時,或遊戲重新開始時設定 Game,以及Engine中的Declarer, Dummy, Lead, KingSuit
func (g *Game) SetGamePlayInfo(declarer, dummy, firstLead, kingSuit uint8) {

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

func bidHistoryItemsToProto(items []*bidItem) *cb.BidHistoryBoard {

	fmt.Printf("bidHistoryToProto there are have %d bid item \n", len(items))

	//Pass, Db叫品
	var byPassLineindicator = func(b CbBid, line uint8) uint32 {
		switch b {
		case Pass1, Pass2, Pass3, Pass4, Pass5, Pass6, Pass7, Db1, Db2, Db3, Db4, Db5, Db6, Db7, Db1x2, Db2x2, Db3x2, Db4x2, Db5x2, Db6x2, Db7x2:
			//前端看到是0必須略不顯示線位
			return uint32(0)
		default:
			return uint32(line)
		}
	}

	board := &cb.BidHistoryBoard{
		Rows: make(map[uint32]*cb.BidHistoryItems),
	}

	var (
		line uint32 = 0
		idx  uint32 = 0
		suit string
	)

	board.Rows[line] = &cb.BidHistoryItems{
		Columns: make([]*cb.BidHistoryItem, 0, 4),
	}

	for i := range items {

		idx = uint32(i)

		// 一個row有四個 column
		if (len(board.Rows[line].Columns)+1)%5 == 0 {
			if idx > uint32(0) {
				line = line + 1
			}
			board.Rows[line] = &cb.BidHistoryItems{
				Columns: make([]*cb.BidHistoryItem, 0, 4),
			}
		}

		suit = fmt.Sprintf("%s", items[idx].value)

		board.Rows[line].Columns = append(board.Rows[line].Columns, &cb.BidHistoryItem{
			Line:       byPassLineindicator(items[idx].value, items[idx].line),
			SuitString: suit[1:],
		})
	}
	return board
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

	//一被點擊,就停止四家正在執行的gauge
	err := g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameOP, &pb.OP{Type: pb.SceneType_game_gauge_stop}, pb.SceneType_game)
	if err != nil {
		g.log.Wrn(fmt.Sprintf("斷線:%s", err.Error()))
	}

	bidHistories, nextLimitBidding, db1, db2 := g.engine.GetNextBid(currentBidder.Zone8, currentBidder.Bid8)

	complete, needReBid := g.engine.IsBidFinishedOrReBid()

	var payload = payloadData{PayloadType: ProtobufType}

	switch complete {
	case false: //仍在競叫中
		//移動環形,並校準座位
		next := g.SeatShift(currentBidder.Zone8)

		//叫牌開始,開始設定這局Engine位置
		g.setEnginePlayer(next)

		//TODO: 轉換bidhistoy 到 proto

		//第一個參數: 表示下一個開叫牌者 前端(Player,觀眾席)必須處理
		//第二個參數: 禁叫品項,因為是首叫所以禁止叫品是 重要 zeroBid 前端(Player,觀眾席)必須處理
		//第三個參數: 上一個叫牌者
		//第四個參數: 上一次叫品

		notyBid := cb.NotyBid{
			BidItems:       bidHistoryItemsToProto(bidHistories),
			Bidder:         uint32(next),
			BidStart:       uint32(nextLimitBidding),
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
		g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameNotyBid, &notyBid, pb.SceneType_game) //廣播Public
		time.Sleep(time.Millisecond * 400)

		payload.Player = next                                                        //指定傳送給 bidder 開叫
		g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateNotyBid, payload) //私人Private

	case true: //競叫完成
		switch needReBid {
		case true: //重新洗牌,重新競叫

			//清除叫牌紀錄
			// moszorn 重要: 一並清除 bidHistories
			g.engine.ClearBiddingState()

			// StartOpenBid會更換新一局,因此玩家順序也做了更動
			bidder := g.start()
			g.SeatShift(bidder)
			g.setEnginePlayer(bidder)

			notyBid := cb.NotyBid{
				BidOrder: &cb.BidOrder{
					Headers: g.GetBidOrder(),
				},
				Bidder:   uint32(bidder),
				BidStart: uint32(valueNotSet), /* 坑:重新競叫前端使用ValueNotSet重新叫訊號*/
				//LastBidderName: fmt.Sprintf("%s-%s", CbSeat(currentBidder.Zone8), currentBidder.Name),
				//LastBid:        fmt.Sprintf("%s", CbBid(currentBidder.Bid8)),
				Double1: uint32(db1.value),
				Double2: uint32(db2.value),
				Btn:     cb.NotyBid_disable_all,
			}
			payload.ProtoData = &notyBid

			//Public廣播
			//if err := g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameNotyBid, payload, pb.SceneType_game); err != nil {
			if err := g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameNotyBid, &notyBid, pb.SceneType_game); err != nil {
				//TODO 清空當前該遊戲桌在Server上的狀態
				g.log.Dbg("GamePrivateNotyBid[重新洗牌,重新競叫錯誤]", slog.String(".", err.Error()))
				g.engine.ClearBiddingState()
			}

			time.Sleep(time.Second * 1)
			g.roomManager.SendShowPlayersCardsOut() //四家攤牌

			time.Sleep(time.Second * 3) //三秒後重新發新牌
			g.roomManager.SendDeal()    //重發牌

			payload.Player = bidder
			g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateNotyBid, payload) //Private 指定傳送給 bidder 開叫

		case false: //競叫完成,遊戲開始

			//這裡開始, 補上最後一個NotyBid(最後一個PASS)
			const MaxUint32 = ^uint32(0) // 4294967295
			notyBid := cb.NotyBid{
				BidStart: MaxUint32, //代表最後的Pass叫
			}
			g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameNotyBid, &notyBid, pb.SceneType_game) //廣播補上最後競叫的PASS
			//-------------------------------------------------

			lead, declarer, dummy, suit, finallyBidding, err := g.engine.GameStartPlayInfo()

			g.SetGamePlayInfo(declarer, dummy, lead, suit)

			if err != nil {
				if errors.Is(err, ErrUnContract) {
					g.log.Wrn("GamePrivateNotyBid[競叫完成,遊戲開始]錯誤", slog.String(".", fmt.Sprintf("合約有問題,只能在合約確定才能呼叫GameStartPlayInfo,%s", err.Error())))
					//TODO 紀錄 log
					return
				}
			}
			g.engine.ClearBiddingState()

			// 向前端發送清除Bidding UI, 並停止(terminate)四家gauge, 並補上競叫歷史紀錄最後一個PASS
			var clearScene = pb.OP{
				Type:     pb.SceneType_game_clear_scene,
				RealSeat: uint32(currentBidder.Zone8),
			}
			payload.ProtoData = &clearScene
			g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameOP, &clearScene, pb.SceneType_game)

			//TODO 未來 工作
			//todo zorn 這裡記住 RoundSuitKeep, 也是第一次紀錄RoundSuitKeep的地方
			//以首引生成 RoundSuit keep
			//g.roundSuitKeeper = NewRoundSuitKeep(lead)

			//移動環形,並校準座位
			g.setEnginePlayer(g.SeatShift(lead))

			//送出首引封包
			// 封包位元依序為:首引, 莊家, 夢家, 合約王牌,王牌字串, 合約線位, 線位字串
			contractLeading := cb.Contract{
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

			slog.Debug("GamePrivateNotyBid[競叫完成,遊戲開始]", slog.String(fmt.Sprintf("莊:%s  夢:%s  引:%s", CbSeat(declarer), CbSeat(dummy), CbSeat(lead)), fmt.Sprintf("花色: %s   合約: %s   賭倍: %s ", CbSuit(suit), finallyBidding.contract, finallyBidding.dbType)))

			//廣播給三家告知合約,首引是誰
			//payload.ProtoData = &contractLeading
			//g.roomManager.SendPayloadTo3PlayersByExclude(ClnRoomEvents.GameFirstLead, payload, lead)
			g.roomManager.SendPayloadTo3PlayersByExclude(ClnRoomEvents.GameFirstLead, &contractLeading, lead)

			//向夢家亮莊家牌
			payload.ProtoData = &cb.PlayersCards{
				Seat: uint32(declarer), /*亮莊家牌*/
				Data: map[uint32][]uint8{
					uint32(dummy): g.deckInPlay[declarer][:], /*向夢家亮莊家牌*/
				},
			}
			payload.Player = dummy
			g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateShowHandToSeat /*向夢家亮莊家的牌*/, payload) //私人Private

			time.Sleep(time.Millisecond * 400)

			//通知首引為下一個出牌者,並開啟其首引gauge與call back
			leadNotice := new(cb.PlayNotice)
			leadNotice.Seat = uint32(lead)
			leadNotice.CardMinValue, leadNotice.CardMaxValue, leadNotice.TimeoutCardValue, _ = g.AvailablePlayerPlayRange(lead, true)
			leadNotice.NumOfCardPlayHitting = uint32(1) // 首引為第一次點擊
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
	slog.Debug("首引遊戲資訊", slog.String("Declarer", fmt.Sprintf("%s", CbSeat(uint8(g.Declarer)))), slog.String("Dummy", fmt.Sprintf("%s", CbSeat(uint8(g.Dummy)))), slog.String("Lead", fmt.Sprintf("%s", CbSeat(uint8(g.Lead)))), slog.String("Defender", fmt.Sprintf("%s", CbSeat(uint8(g.Defender)))), slog.String("result", fmt.Sprintf("首引%s 打出 %s , leadPlayer.NumOfCardPlayHitting: %d", CbSeat(leadPlayer.Zone8), CbCard(leadPlayer.Play8), leadPlayer.NumOfCardPlayHitting)))

	firstPlayHitting := leadPlayer.NumOfCardPlayHitting
	if firstPlayHitting != uint32(1) {
		//TODO: 記log或回復錯誤
		slog.Warn("首引出牌點擊數錯誤", slog.Int("點擊數應為1,但收到", int(leadPlayer.NumOfCardPlayHitting)))
		panic("首引點數錯誤")
	}

	var (
		nextPlayer                    = g.SeatShift(leadPlayer.Zone8)
		nextRealPlaySeat, isAgentPlay = g.playTurn(nextPlayer)

		//重要: 要先同步server的牌組,後面才會正確
		refresh, _ = g.PlayOutHandRefresh(leadPlayer.Zone8, leadPlayer.Play8)

		nextNotice = &cb.PlayNotice{
			IsPlayAgent:          isAgentPlay,                  /*若為莊打夢,則前端要修正seat為封包發送者(nextRealPlaySeat)*/
			Dummy:                uint32(g.Dummy),              /*前端若 IsPlayAgent為 true, 必須以 Dummy為 View (莊家要開啟夢家View)*/
			Seat:                 uint32(nextRealPlaySeat),     /*夢家,但實際是莊家 (設定gauge)*/
			NumOfCardPlayHitting: firstPlayHitting + uint32(1), /*下一次點擊應為2*/
		}

		//首引出牌後, 下一個出牌者是夢家,但實際上是莊家
		// 所以對於首引出牌, NextSeat表示應為
		//  首引夥伴 - seat: leadPlayer.Zone, NextSeat: nextRealPlaySeat
		//  夢家 -    seat: leadPlayer.Zone, NextSeat: nextRealPlaySeat
		//  莊家 -    seat: leadPlayer.Zone, NextSeat: nextRealPlaySeat

		coverCardAction = &cb.CardAction{
			Type:          cb.CardAction_play,
			CardValue:     leadPlayer.Play,
			Seat:          leadPlayer.Zone,          /*停止的Gauge*/
			NextSeat:      uint32(nextRealPlaySeat), /*下一家Gauge 夢家,但實際是莊家 (設定gauge)*/
			IsCardCover:   true,                     /*蓋牌打出*/
			PlaySoundName: g.engine.GetCardSound(leadPlayer.Play8),
		}
		faceCardAction = &cb.CardAction{
			AfterPlayCards: refresh, /*出牌後首引重整牌組*/
			Type:           cb.CardAction_play,
			CardValue:      coverCardAction.CardValue, // 因為已經打出所以..
			Seat:           coverCardAction.Seat,      /*停止的Gauge*/
			NextSeat:       coverCardAction.NextSeat,  /*下一家Gauge, 夢家,但實際是莊家 (設定gauge)*/
			IsCardCover:    false,                     /*明牌打出*/
			PlaySoundName:  coverCardAction.PlaySoundName,
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

	g.setEnginePlayer(nextPlayer)

	dummyCards := g.deckInPlay[uint8(g.Dummy)][:]

	// memo 1)向三家亮出夢家牌 (
	g.roomManager.SendDummyCardsByExcludeDummy(ClnRoomEvents.GamePrivateShowHandToSeat, &dummyCards, uint8(g.Dummy))

	//首引出牌
	g.roomManager.SendPayloadToOneAndPayloadToOthers(ClnRoomEvents.GameCardAction, commonPayload, specialPayload, leadPlayer.Zone8)

	//儲存出牌紀錄
	g.savePlayCardRecord(leadPlayer.Zone8, leadPlayer.Play8)
	// 初始回合出牌範圍
	g.SetRoundAvailableRange(leadPlayer.Play8) //回合首打制定回合出牌範圍

	// memo 2) 通知下家出牌(注意:首引後的出牌者是莊家要打夢家的牌,所以是計算夢家可出牌範圍)
	//    (2.1)下一位出牌者可打出的牌 range (Max,min)
	//    (2.2)下一位若過了指定時間(gauge),自動打出哪張牌 (必定在range間,否則索引第一張)
	//出夢家(nextPlayer)可出牌範圍
	nextNotice.CardMinValue, nextNotice.CardMaxValue, nextNotice.TimeoutCardValue, _ = g.AvailablePlayerPlayRange(nextPlayer, false)
	slog.Debug(fmt.Sprintf("夢家 %s 出牌範圍", CbSeat(nextPlayer)), slog.String("range", fmt.Sprintf("%s ~ %s  , timeout: %s ", CbCard(nextNotice.CardMinValue), CbCard(nextNotice.CardMaxValue), CbCard(nextNotice.TimeoutCardValue))))

	//通知莊家出夢家牌
	g.nextPlayNotification(nextNotice, nextRealPlaySeat)

	return nil
}

// 傳入玩家座位,回傳實際出牌的玩家(例如: 下一位是夢家,但實際出牌的是莊家)
// bool 表示是否是莊打夢, true表莊打夢,或可以理解為夢家的turn但是莊家出牌, false:表莊打莊,防打防, 就是是否是代理的意思
func (g *Game) playTurn(player uint8) (uint8, bool) {
	switch CbSeat(player) {
	case g.Dummy:
		return uint8(g.Declarer), true
	default:
		return player, false
	}
}

// SetRoundAvailableRange 設定回合可出牌範圍(roundMin, roundMax)
// 傳入參數 firstPlay表示首打出的牌
func (g *Game) SetRoundAvailableRange(firstPlay uint8) {
	roundRange := GetRoundRangeByFirstPlay(firstPlay)
	g.roundMin = roundRange[0]
	g.roundMax = roundRange[1]
}

// AvailablePlayerPlayRange 玩家可出牌範圍最大值,最小值,依照 roundMin, roundMax決定
// player 取得玩家有效出牌,
// isRoundStart 是否是新回合的開始: 新回合允許玩家手中所有的出牌範圍，且timeout是手上最小張有效牌
func (g *Game) AvailablePlayerPlayRange(player uint8, isRoundStart bool) (minimum, maximum, timeout, timeoutCardIndex uint32) {
	var (
		hitAvailable = false //
		hitFirst     = false
		hand         = g.deckInPlay[player]

		//為了要讓底下if判斷式成立,所以將 m, M 分別設定到極限
		m, M = spadeAce + uint8(1), uint8(BaseCover)
	)

	//預設隨便出都可 (表示沒有可出的花色,可以任意出)
	minimum, maximum = uint32(club2), uint32(spadeAce)

	for i := range hand {
		if hand[i] == uint8(BaseCover) {
			continue
		}

		// 陣列中第一張有效牌,與其索引
		if !hitFirst {
			hitFirst = true
			//先設定,若time gauge 時間到時,要出的牌 (一定是陣列中第一張有效牌)
			timeout = uint32(hand[i])
			timeoutCardIndex = uint32(i)
		}

		switch isRoundStart {
		case false: //可出牌範圍受 g.roundMin, g.roundMax限制
			if g.roundMin <= hand[i] && g.roundMax >= hand[i] {
				//發現 player 手頭上有牌
				hitAvailable = true
				if M < hand[i] {
					M = hand[i]
				}
				if m > hand[i] {
					m = hand[i]
					timeoutCardIndex = uint32(i)
				}
			}
		case true:
			//發現 player 手頭上有牌
			hitAvailable = true
			// isRoundStart 則 g.roundMin, g.roundMax 不予考慮
			if m == spadeAce+uint8(1) { // m若沒設定,則第一張牌即最小牌,也是 timeout 牌
				m = hand[i]
				timeoutCardIndex = uint32(i)
			}
			if hand[i] > M {
				M = hand[i]
			}
		}

	}
	//手頭上有牌,則限定可出範圍最大值與最小值
	if hitAvailable {
		minimum = uint32(m)
		maximum = uint32(M)
		timeout = minimum
	}

	//slog.Debug(fmt.Sprintf("%s可出牌區間", CbSeat(player)), slog.String("FYI", fmt.Sprintf("%s  ~  %s   timeout: %s (索引值:%d)", CbCard(minimum), CbCard(maximum), CbCard(timeout), timeoutCardIndex)))
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

	if !cardAction.IsHoverTriggerByDeclarer {
		g.log.Wrn("GamePrivateCardHover", slog.String(".", fmt.Sprintf("觸發者應該是莊(%s)但觸發是 %s", g.Declarer, CbSeat(cardAction.Seat))))
		return nil
	}

	if cardAction.Type == cb.CardAction_play {
		g.log.Wrn("GamePrivateCardHover", slog.String(".", fmt.Sprintf(" %s  型態應該是hover/out但傳入型態是Play", CbCard(cardAction.CardValue))))
		return nil
	}
	//server trigger by pass 回前端夢家
	cardAction.IsHoverTriggerByDeclarer = false

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

	slog.Debug("出牌",
		slog.String("FYI",
			fmt.Sprintf("%s (%s) 打出 %s 牌 %s , (%s)的牌被打出, 目前NumOfCardPlayHitting: %d",
				CbSeat(clickPlayer.Zone8),
				clickPlayer.Name,
				CbSeat(clickPlayer.PlaySeat),
				CbCard(clickPlayer.Play8),
				CbSeat(clickPlayer.PlaySeat8),
				clickPlayer.NumOfCardPlayHitting)))

	//這輪play第幾張出牌, hitting=> 0(表示四人已經牌以打出), 1(表示1人出牌), 2(表示2人出牌), 3(表示三人出牌)
	var (
		cardPlayHitting uint32  = clickPlayer.NumOfCardPlayHitting % uint32(4) //至少從2開始參考GamePrivateFirstLead
		refresh         []uint8                                                //(出牌者)出牌後的refresh
	)

	// hitting=> 0(表示四人已經牌以打出), 1(表示1人出牌), 2(表示2人出牌), 3(表示三人出牌)
	if cardPlayHitting < uint32(0) || cardPlayHitting > uint32(3) {
		slog.Warn("出牌點擊問題", slog.String("FYI", fmt.Sprintf("出牌點擊數至少要大於2且小於4,實際為%d", cardPlayHitting)))
		panic("出牌點擊問題")
	}

	//一被點擊,就停止四家正在執行的gauge
	err := g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameOP, &pb.OP{Type: pb.SceneType_game_gauge_stop}, pb.SceneType_game)
	if err != nil {
		g.log.Wrn("斷線", slog.String(".", err.Error()))
	}

	//第一張出牌必須執行限定回合出牌範圍,否則底下求得可出牌範圍(AvailablePlayerPlayRange)會無效
	if cardPlayHitting == 1 {
		g.SetRoundAvailableRange(clickPlayer.Play8) //回合首打制定回合出牌範圍
	}

	// 重要 Step0 儲存玩家出牌紀錄
	g.savePlayerCardRecord(clickPlayer)

	// 重要 Step1 更新最後出牌者手上的牌組, 因為最後出牌的玩家要refresh手上牌
	refresh, _ = g.PlayOutHandRefresh(clickPlayer.PlaySeat8, clickPlayer.Play8)

	var (
		isLastPlay       = cardPlayHitting == 0 //四否最後第四張出牌
		nextPlayer       uint8                  // 遊戲上下一個玩家
		nextRealPlaySeat uint8                  //實際上下一個出牌者
		// 發送給判斷 isLastPlay後的 nextRealPlaySeat
		nextPlayNotice = &cb.PlayNotice{
			NumOfCardPlayHitting: clickPlayer.NumOfCardPlayHitting + uint32(1),
			IsPlayAgent:          false, /*下面playTurn判斷式判斷後設定*/
		}
	)

	if isLastPlay /*該回合已出四張牌*/ {

		//坑: 結算前,要先設定最後出牌得玩家
		g.setEnginePlayer(clickPlayer.PlaySeat8)

		nextPlayer = g.engine.GetPlayResult(g.eastCard, g.southCard, g.westCard, g.northCard, g.KingSuit)

		//TODO: 計算回合結果
		slog.Debug("回合結束", slog.String("結果", fmt.Sprintf("東: %s , 南:  %s ,西: %s , 北: %s , 勝出: %s", CbCard(g.eastCard), CbCard(g.southCard), CbCard(g.westCard), CbCard(g.northCard), CbSeat(nextPlayer))))

		//restore所有出牌 BaseCover
		g.resetPlayCardRecord()

	} else {
		nextPlayer = g.SeatShift(clickPlayer.PlaySeat8) //一定要使用 PlaySeat, 因為莊打夢的關係
		g.setEnginePlayer(nextPlayer)
	}
	//重要
	//   在此知道下一家出牌者是否是夢家
	nextRealPlaySeat, nextPlayNotice.IsPlayAgent = g.playTurn(nextPlayer)
	nextPlayNotice.Dummy = uint32(g.Dummy)
	nextPlayNotice.CardMinValue, nextPlayNotice.CardMaxValue, nextPlayNotice.TimeoutCardValue, _ = g.AvailablePlayerPlayRange(nextPlayer, isLastPlay)
	nextPlayNotice.Seat = uint32(nextRealPlaySeat)

	slog.Debug("NextNotice",
		slog.String("FYI",
			fmt.Sprintf("新回合:%t  下一個玩家 %s 是否代理(%t), 實際出牌: %s ,出牌範圍: %s  ~ %s  , 自動出牌: %s ",
				isLastPlay,
				CbSeat(nextPlayNotice.Seat),
				nextPlayNotice.IsPlayAgent,
				CbSeat(nextRealPlaySeat),
				CbCard(nextPlayNotice.CardMinValue),
				CbCard(nextPlayNotice.CardMaxValue),
				CbCard(nextPlayNotice.TimeoutCardValue))))

	var (
		/*
		 CardAction 主要作用在向前段要求執行
		   1. mouse hover/out (莊,夢)
		   2. 停止上一家gauge
		   3. 開始執行下一家gauge
		   4. 執行出牌動作,所以CardAction封包必須送到每位玩家的手上,以便進行前端動態效果)
		*/
		// CONVENTION: ca1 通常用於 refresh , 明牌回覆
		ca1 = &cb.CardAction{
			AfterPlayCards: nil, /*後面決定,莊打夢,莊打莊,防打防*/
			Type:           cb.CardAction_play,
			CardValue:      clickPlayer.Play,
			Seat:           clickPlayer.PlaySeat,
			NextSeat:       uint32(nextPlayer), /*由上面判斷isLastPlay來決定*/
			IsCardCover:    false,              /*後面決定,莊打夢,莊打莊,防打防*/
			PlaySoundName:  g.engine.GetCardSound(clickPlayer.Play8),
		}
		// CONVENTION: ca2 通常用於沒有refresh, 暗牌回覆
		ca2 = &cb.CardAction{
			AfterPlayCards: nil, /*後面決定,莊打夢,莊打莊,防打防*/
			Type:           ca1.Type,
			CardValue:      ca1.CardValue,
			Seat:           ca1.Seat,
			NextSeat:       ca1.NextSeat,
			IsCardCover:    true, /*後面決定,莊打夢,莊打莊,防打防*/
			PlaySoundName:  ca1.PlaySoundName,
		}
		payload1 = payloadData{
			PayloadType: ProtobufType,
		}
		payload2 = payloadData{
			PayloadType: ProtobufType,
		}
		//重要: 這個 array 是預先將SendPayload涵式集中,在最後透過IsLastPlay判斷,進行一次性同時送出
		//     ,藉以達到畫面執行gauge效果一致
		sendPayloadsFuncsByIsLastPlay = make([]func(), 0, 3)
	)

	// 注意: 透過playTurn,得知當前(目前)出牌者 是否是夢家出牌
	switch _, isDummyTurn := g.playTurn(clickPlayer.PlaySeat8); isDummyTurn {

	case false /*莊出牌莊自己的牌, 防出牌防家自己的牌*/ :

		switch clickPlayer.Zone8 {
		case uint8(g.Declarer) /*莊出牌莊*/ :

			//1. 設定 莊,夢封包: 1) refresh 2)明牌CardAction
			ca1.AfterPlayCards = refresh
			ca1.IsCardCover = false

			//2. 防家封包: 1)沒有 refresh 2) 蓋牌CardAction
			ca2.IsCardCover = true

			//[H] g.roomManager.SendPayloadToDefendersToAttacker(ClnRoomEvents.GameCardAction, ca2, ca1, uint8(g.Declarer), uint8(g.Dummy))

			//儲存 Payload Send 單元
			sendPayloadsFuncsByIsLastPlay = append(sendPayloadsFuncsByIsLastPlay, func() {
				//[H]
				g.roomManager.SendPayloadToDefendersToAttacker(ClnRoomEvents.GameCardAction, ca2, ca1, uint8(g.Declarer), uint8(g.Dummy))
			})

		default /*防家出牌防*/ :

			//1. 回覆打出者防家 refresh, 明牌
			ca1.AfterPlayCards = refresh
			ca1.IsCardCover = false
			payload1.Player = clickPlayer.Zone8
			payload1.ProtoData = ca1
			//[U]	g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GameCardAction, payload1) //私人Private

			// 2.回覆莊,夢,防夥伴 (a)沒有refresh (b)蓋牌CardAction
			if CbSeat(nextPlayer) == g.Dummy {
				//注意: 下一位輪到夢家出牌時
				//送出出牌結果給 防家的夥伴 及 莊家
				partner, _ := GetPartnerByPlayerSeat(clickPlayer.Zone8)
				//[X] g.roomManager.SendPayloadToTwoPlayer(ClnRoomEvents.GameCardAction, ca2, partner, uint8(g.Declarer))

				//送出出牌結果給專門給夢家, CbSeat(nextPlayer)為夢家,必須另送一個專門封包給夢家
				// 好讓夢家的前端執行莊家gauge, 這樣的想法是,下一次送出CardAction時,可以無縫的停掉夢家的gauge,而不用再改code
				payload2.Player = uint8(g.Dummy)
				payload2.ProtoData = ca2
				ca2.NextSeat = uint32(g.Declarer) //下一位若是夢家,自動轉成莊家
				//[Y] g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GameCardAction, payload2)

				//儲存 Payload Send 單元
				sendPayloadsFuncsByIsLastPlay = append(sendPayloadsFuncsByIsLastPlay, func() {
					//[U]
					g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GameCardAction, payload1) //私人Private
					//[X]
					g.roomManager.SendPayloadToTwoPlayer(ClnRoomEvents.GameCardAction, ca2, partner, uint8(g.Declarer))
					//[Y]
					g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GameCardAction, payload2)
				})

			} else {

				//若這次防家打出後 注意: 下一位輪到的不是夢家出牌時
				//莊,防家夥伴,夢家 一封包送三個
				exclude := clickPlayer.Zone8
				//[W] g.roomManager.SendPayloadTo3PlayersByExclude(ClnRoomEvents.GameCardAction, ca2, exclude)

				//儲存 Payload Send 單元
				sendPayloadsFuncsByIsLastPlay = append(sendPayloadsFuncsByIsLastPlay, func() {
					//[U]
					g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GameCardAction, payload1) //私人Private
					//[W]
					g.roomManager.SendPayloadTo3PlayersByExclude(ClnRoomEvents.GameCardAction, ca2, exclude)
				})
			}

		} /*eof*/
	case true /*莊打夢*/ :
		if uint8(g.Declarer) != clickPlayer.Zone8 {
			slog.Warn("莊家身份錯誤", slog.String("FYI", fmt.Sprintf("莊應為:%s ,但收到 %s 打出 %s", g.Declarer, CbSeat(clickPlayer.Zone8), CbCard(clickPlayer.Play8))))
			return nil
		}
		if uint8(g.Dummy) != clickPlayer.PlaySeat8 {
			slog.Warn("夢家身份錯誤", slog.String("FYI", fmt.Sprintf("夢應為:%s ,但收到 %s 打出 %s", g.Dummy, CbSeat(clickPlayer.PlaySeat8), CbCard(clickPlayer.Play8))))
			return nil
		}
		// 莊打夢,只要是夢家,大家仍都可以看到夢的明牌,與所出的牌,所以payload是一樣的
		//2. 防家依然會看到夢的明牌
		ca1.AfterPlayCards = refresh //四家到會看到夢家refresh
		ca1.IsCardCover = false      //四家都會看到夢家打出明牌
		//[Z] g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameCardAction, ca1, pb.SceneType_game)

		sendPayloadsFuncsByIsLastPlay = append(sendPayloadsFuncsByIsLastPlay, func() {
			//[Z]
			g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameCardAction, ca1, pb.SceneType_game)
		})

	} /*eof*/

	switch isLastPlay /*回合結算*/ {
	case false:
		fmt.Printf("非回合結算 sendPayloadsFuncsByIsLastPlay: %v\n", sendPayloadsFuncsByIsLastPlay)
		//注意：同時送出四家  payload
		for idx := range sendPayloadsFuncsByIsLastPlay {
			sendPayloadsFuncsByIsLastPlay[idx]()
		}

		//通知下一位出牌
		g.nextPlayNotification(nextPlayNotice, nextRealPlaySeat)

	case true:
		//遊戲結束
		if clickPlayer.NumOfCardPlayHitting == 52 {

			//注意：同時送出四家  payload
			for idx := range sendPayloadsFuncsByIsLastPlay {
				sendPayloadsFuncsByIsLastPlay[idx]()
			}

			//遊戲結束
			g.GameSettle(clickPlayer)

		} else { //表示回合結束

			fmt.Printf("回合結算 sendPayloadsFuncsByIsLastPlay: %v\n", sendPayloadsFuncsByIsLastPlay)
			//注意：同時送出四家  payload
			for idx := range sendPayloadsFuncsByIsLastPlay {
				sendPayloadsFuncsByIsLastPlay[idx]()
			}
			time.Sleep(time.Millisecond * 700)

			//TODO: 送出清除桌面打出的牌,準備下一輪開始
			err := g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameOP, &pb.OP{Type: pb.SceneType_game_round_clear}, pb.SceneType_game)
			if err != nil {
				//TODO: log goes here這裡絕不能出錯
				panic(err)
				//廣播有人GG
			}

			//避免玩家快速再次點擊下一張出牌,導致前端螢幕還沒開始清除上一回合桌面,發生不必要的頁面問題
			//下一輪首打通知
			time.Sleep(time.Millisecond * 500) // 重要 的延遲時間,到時候上時還要再加上網路傳輸的延遲
			g.nextPlayNotification(nextPlayNotice, nextRealPlaySeat)
		}
	}
	return nil
}

// 通知下一位出牌者準備出牌,(nextRealPlayer 要設定好,永遠不會輪到夢家)
func (g *Game) nextPlayNotification(nxtNotice *cb.PlayNotice, nextRealPlayer uint8) {
	//莊打牌不必通知夢家,夢家只需要CardAction通知,因為夢家不需要打牌(Notice)
	g.roomManager.SendPayloadToPlayer(ClnRoomEvents.GamePrivateCardPlayClick, payloadData{
		Player:      nextRealPlayer,
		ProtoData:   nxtNotice,
		PayloadType: ProtobufType,
	}) //私人Private

}

// GameSettle 遊戲已出滿52張牌,進行遊戲結算, lastPlayer最後一個出牌玩家
func (g *Game) GameSettle(lastPlayer *RoomUser) {

	//   Step0. 儲存出牌紀錄
	g.savePlayerCardRecord(lastPlayer)

	// TODO: 現在還不知道如何計算結果,需要加入橋牌社了解
	//   Step1. 回合結束,結算遊戲,告知前端,計算該局遊戲結果

	//   Step2. 送出清除桌面打出的牌,準備下一輪開始
	time.Sleep(time.Second * 2)
	g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameOP, &pb.OP{Type: pb.SceneType_game_round_clear}, pb.SceneType_game)

	// TODO:
	//    Step3. 廣播該局結果

	//   Step4. 清空該局結果UI,清空桌面
	time.Sleep(time.Second * 2)
	//TODO: 底下已經有OP sceneType了
	//g.roomManager.SendPayloadToPlayers(ClnRoomEvents.GameOP, &pb.OP{Type: pb.SceneType_game_result_clear}, pb.SceneType_game)

	//   Step5 重新競叫開始
	g.roomManager.SendGameStart()
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

func (g *Game) savePlayerCardRecord(player *RoomUser) {
	//Step0. 儲存出牌紀錄
	switch player.PlaySeat8 {
	case player.Zone8: //莊打莊,防打防 [PlaySeat8 == Zone8]
		g.savePlayCardRecord(player.Zone8, player.Play8)
	default: //莊打夢  [ PlaySeat8 != Zone8]
		g.savePlayCardRecord(player.PlaySeat8, player.Play8)
	}
}

// savePlayCardRecord 紀錄玩家出牌,方便回合終了結果計算
func (g *Game) savePlayCardRecord(player, card uint8) {
	switch player {
	case uint8(east):
		g.eastCard = card
	case uint8(south):
		g.southCard = card
	case uint8(west):
		g.westCard = card
	case uint8(north):
		g.northCard = card
	default:
		//TODO:
		slog.Warn("玩家出牌紀錄發生問題",
			slog.String("player", fmt.Sprintf("%s", CbSeat(player))),
			slog.String("牌", fmt.Sprintf("%d ( %s )", card, CbCard(card))))
	}
}

func (g *Game) resetPlayCardRecord() {
	g.eastCard = uint8(BaseCover)
	g.westCard = uint8(BaseCover)
	g.northCard = uint8(BaseCover)
	g.southCard = uint8(BaseCover)
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
