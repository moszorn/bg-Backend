package game

import (
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	utilog "github.com/moszorn/utils/log"
)

// 隨機開叫產出 TODO:
func randomSeat() uint8 {
	return playerSeats[rand.Int31n(4)]
}

type Engine struct {
	//locker sync.RWMutex

	// Record 每場遊戲最後計分所需要的資訊
	Record *record

	//四家Suit叫牌時間紀錄, Key: CbSeat | CbSuit , value: 時間(用來比對玩家叫品先後順序,以找出莊家)
	bidHistories map[uint8]time.Time

	/* 重要 預設值ValueNotSet,這個屬性是唯一一處知道幾線叫品的屬性,但無法從這個屬性得知誰是莊,夢,因為可能兩家都同時合約
	memo 如何從 Contract 知道是誰最後叫到合約,合約又是什麼? 注意: 但不能知道莊家是誰
	 seat = CbSeat(Contract & seatMark8 )
	 幾線合約  = CbBid(egn.Contract & valueMark8)
	*/
	Contract uint8 //注意 Contract 是CbSeat + CbBid

	// 合約代表的王牌 (game.CLUB,game.DIAMOND,game.HEART,game.SPADE,game.TRUMP)
	contractSuit uint8 //注意 trumpSuit 是CbSuit,無法從contract知道幾線叫品, contract只代表合約中的王牌

	//王張區間, 在計算首引(getGameFirstLead)時計算出王張區間,代表本局合法的王牌有哪幾張
	trumpRange CardRange

	// 表示三家pass,一家有叫品(Contract),表示叫到王牌競叫結束
	// 未來 當遊戲桌關閉時,記得一同關閉channel 以免leaking 重要
	passBuffered chan struct{}

	//lastBid 表示上一次叫品,其初始叫品為 0x1 (參考7線叫品值),叫牌過程中,當前叫品絕對不會小於或等於lastBid,除了 PASS bid外
	lastBid uint8

	//本局莊家,計算GameResult會用到
	declarer uint8
	//本局夢家,計算GameResult會用到
	dummy uint8
	//.................................................)
	// 底下用於叫品成立時,面對double,redouble, 還能keep lastPlayer
	//初始值是0
	// 情境ㄧ: 連續收到 db, dbx2, pass, pass, pass
	//  db, doubleKeepLastPassPlayer + 1 (加1)
	//  dbx2, doubleKeepLastPassPlayer + 1 (再加1)
	//  PASS, 則 doubleKeepLastPassPlayer + 1 (再加1),之後再收到PASS就不在累加
	// 情境二: 連續收到 db, pass, pass, pass
	//  db,   doubleKeepLastPassPlayer + 1 (加1)
	//  PASS, doubleKeepLastPassPlayer + 1 (再加1)
	//  PASS, doubleKeepLastPassPlayer + 1 (再加1),之後再收到PASS就不在累加
	// 情境三: 收到db後,若收到其它非 dbx2, pass 叫品時, doubleBidCounter必須歸0
	doubleBidCounter int

	// 初始值valueNotSet [ east(0,0x0), south(64,0x40), west(128,0x80), north(192,0xC0), valueNotSet (136,0x88)]
	// 只有在 doubleBidCounter有 +1 的動作時, 才需要(且必須要)設定 doubleKeepLastPassPlayer, 當 doubleBidCounter>=3時,doubleKeepLastPassPlayer就不可再進行設定,直到 doubleBidCounter歸零後
	// 不論 doubleBidCounter歸零否, doubleKeepLastPassPlayer只會判斷 doubleBidCounter是否 >= 3,才不會進行設定
	doubleKeepLastPassPlayer uint8

	//.................................................)
	//表示當前叫牌玩家,或出牌玩家座位
	currentPlay uint8
	//表示下一個叫牌,下一個出牌者座位
	nextPlay uint8
}

func newEngine() *Engine {

	egn := &Engine{
		Record: &record{
			isDouble: false,
			dbType:   ZeroSuit,
			contract: BidYet,
		},
		passBuffered: make(chan struct{}, 4),
		bidHistories: make(map[uint8]time.Time),
		trumpRange:   CardRange{},
		lastBid:      0x1,
		Contract:     valueNotSet,
		contractSuit: valueNotSet,
		declarer:     valueNotSet,
		dummy:        valueNotSet,
		currentPlay:  valueNotSet,
		nextPlay:     valueNotSet,
	}

	return egn
}

// SetCurrentSeat 設定當前叫牌者or出牌者
// memo DONE
func (egn *Engine) SetCurrentSeat(seat uint8) {
	//defer egn.locker.Unlock()
	//egn.locker.Unlock()
	egn.currentPlay = seat
}

// SetNextSeat 設定下一位叫牌者or出牌者
// memo DONE
func (egn *Engine) SetNextSeat(seat uint8) {
	//defer egn.locker.Unlock()
	//egn.locker.Unlock()
	egn.nextPlay = seat
}

// ClearGameState Engine 狀態還原
func (egn *Engine) ClearGameState() {}

// ClearBiddingState 競叫底定,四家PASS 或準備重新競叫前執行清除競叫紀錄
// memo DONE
func (egn *Engine) ClearBiddingState() {
	clear(egn.bidHistories)
	//初始叫品為 0x1 (參考7線叫品值)
	egn.lastBid = 0x1
	egn.currentPlay = valueNotSet
	egn.nextPlay = valueNotSet
	egn.Contract = valueNotSet

	egn.doubleKeepLastPassPlayer = valueNotSet
	egn.doubleBidCounter = 0
	// TODO 清空 record
	egn.Record.isDouble = false
	egn.Record.dbType = ZeroSuit
	egn.Record.contract = BidYet

	slog.Debug("ClearBiddingState", slog.Bool("清空還原競叫狀態", true))

	//坑: 清空trumpSuit該在叫牌前執行
	//egn.trumpSuit = valueNotSet
}

// GetGameRole 回傳莊夢座位,莊夢egn.declarer於getGameFirstLead時被設定
// memo DONE
func (egn *Engine) GetGameRole() (declarer uint8, dummy uint8, defender1 uint8, defender2 uint8) {
	switch egn.declarer {
	case east:
		return east, west, north, south
	case west:
		return west, east, north, south
	case south:
		return south, north, east, west
	default:
		//north
		return north, south, east, west
	}
}

// IsBidValue8Valid 叫品是否合法, 傳入參數是不帶位置叫品值
// memo DONE
func (egn *Engine) IsBidValue8Valid(rawBidValue uint8) (isValid bool) {

	// 0x38為最大的7線 ReDouble(參考7線叫品值), uint8不可能是是負值
	if 0x38 < rawBidValue {
		return
	}

	if egn.isZeroBidOrPassBid(rawBidValue) { // bypass PASS bidding
		return true
	}

	//rawBidValue 要比上一次叫品要大(rawBidValue 到此不可能是PASS,因為上面判斷式已經濾掉了)
	return rawBidValue > egn.lastBid
}

// IsLastBidOrReBid 是否叫牌底定,還是需要重新競叫, bidCompleted (競叫底定), bidReDo(重新發牌競叫)
// memo DONE 取代原 IsLastPassOrReshuffle
func (egn *Engine) IsLastBidOrReBid(rawBidValue uint8) (bidCompleted, bidReDo bool) {
	var (
		drainBuffer = func() {
			for len(egn.passBuffered) != 0 {
				<-egn.passBuffered
			}
		}
		isPassBid bool = (rawBidValue-uint8(1))%8 == 0
	)
	//此叫非PASS,清空 pass buffered
	if !isPassBid {
		drainBuffer()
		return
	}

	// 底下是叫品為PASS時,考慮的邏輯

	// 此次叫品為PASS,丟入一個struct到buffer
	egn.passBuffered <- struct{}{}

	pass := len(egn.passBuffered)
	if pass < 3 {
		return
	} else if pass == 3 {
		//eng.Contract 表示仍未開叫,之前全都PASS叫
		if egn.Contract != valueNotSet && !egn.isZeroBidOrPassBid(egn.Contract&valueMark8) {
			//表示第三個pass被叫時,已經有叫品產生, 競叫底定
			bidCompleted = true
			drainBuffer()
			return
		} else {
			//表示第三個pass被叫時,仍無叫品產生,必須等候第四個pass後,重新競叫
			return
		}

	} else if pass > 3 /*四人競叫,結果競叫流標,也就是已經叫了4個PASS了*/ {
		//留局,競叫殘念
		drainBuffer()
		bidCompleted = true
		bidReDo = true //重新競叫
		//清空還原叫牌紀錄
		egn.ClearBiddingState()

	}

	return
}

// cacheBidHistories 將以座位叫的叫品以( CbSeat | CbSuit )形式儲存保留便於後續決定王牌
// memo DONE
func (egn *Engine) cacheBidHistories(cbSeatCbBid uint8) error {

	var (
		//叫者
		bidder = cbSeatCbBid & seatMark8
		//叫品
		bid = cbSeatCbBid & valueMark8
		//合約王
		cbSuit, ok = rawBidSuitMapper[cbSeatCbBid]

		// history 作為儲存競叫記錄項目(item)
		history = bidder | cbSuit
	)
	slog.Debug("cacheBidHistories",
		utilog.Err(errors.New(
			fmt.Sprintf(
				"傳入參數cbSeatCbBid:%d 提取Seat: %d(%s),提取Bid: %d[ %s  ] 並以[ %s OR %s  ]為History cache Key (%d)儲存",
				cbSeatCbBid, bidder, CbSeat(bidder), bid, CbBid(bid), CbSeat(bidder), CbSuit(cbSuit), history))))
	if !ok {
		//TODO 這裡發生不知名合約叫品
		return ErrUnknownBid
	}

	if _, ok = egn.bidHistories[history]; !ok {
		//以座位儲存叫牌紀錄,並附上時搓,到時以時搓比對是哪一座位先以此花色為王,他就是莊
		egn.bidHistories[history] = time.Now()
	}

	//zorn
	egn.dumpBidHistories()
	return nil
}

func (egn *Engine) dumpBidHistories() {
	fmt.Println("-----------------------------------------------------")
	slog.Info("dumpBidHistories[bid cache]", slog.Int("len(bidHistories)", len(egn.bidHistories)))
	if len(egn.bidHistories) == 0 {
		return
	}
	for k, t := range egn.bidHistories {
		fmt.Printf("Key:%d [ bidder:%s bid: %s ] bid time: %s\n", k, CbSeat(k&seatMark8), CbSuit(k&valueMark8), t.Format("04:05"))
	}
	fmt.Println("-----------------------------------------------------")
}

// GameStartPlayInfo 競叫結束,以最後叫pass的玩家座位(lastPassSeat)為參數取得 leasSeat首引, declarerSeat莊家, dummySeat夢家, contractSuit王牌花, contract 合約紀錄(包含是否db,叫品線位)
// memo DONE 取代原 getGameFirstLead
func (egn *Engine) GameStartPlayInfo(lastPassSeat uint8) (leadSeat, declarerSeat, dummySeat, contractSuit uint8, contract record, err error) {
	var (
		ok             bool
		dbgOldPassSeat uint8 = lastPassSeat
	)
	egn.contractSuit = valueNotSet
	if egn.Contract == valueNotSet {
		return valueNotSet, valueNotSet, valueNotSet, valueNotSet, record{}, ErrUnContract
	}

	egn.contractSuit, ok = rawBidSuitMapper[egn.Contract]

	if !ok {
		slog.Error("GameStartPlayInfo", slog.String("FYI", fmt.Sprintf("CbSeat(%s), CbBid(%s)無法對應任何叫品", CbSeat(egn.Contract&seatMark8), CbBid(egn.Contract&valueMark8))), utilog.Err(ErrUnContract))
		return valueNotSet, valueNotSet, valueNotSet, valueNotSet, record{}, ErrUnContract
	}

	slog.Debug("GameStartPlayInfo",
		slog.String("FYI",
			fmt.Sprintf("最後叫PASS的是%d(%s),王牌數值:%d  %s  ",
				lastPassSeat,
				CbSeat(lastPassSeat),
				egn.contractSuit,
				CbSuit(egn.contractSuit))))

	if egn.Record.isDouble {
		//因為賭倍,重設 lastPassSeat
		lastPassSeat = egn.doubleKeepLastPassPlayer
	}

	egn.dumpBidHistories()

	switch lastPassSeat {
	//南北合約局
	case east, west: //最後PASS的是 east, west
		slog.Info("GameStartPlayInfo", slog.Int("Key", int(south|egn.contractSuit)), slog.String("南叫約時間", fmt.Sprintf("%s", egn.bidHistories[south|egn.contractSuit])))
		slog.Info("GameStartPlayInfo", slog.Int("Key", int(north|egn.contractSuit)), slog.String("北叫約時間", fmt.Sprintf("%s", egn.bidHistories[north|egn.contractSuit])))
		switch s, n := egn.bidHistories[south|egn.contractSuit], egn.bidHistories[north|egn.contractSuit]; {
		case !s.IsZero() && n.IsZero(): //南
			fallthrough
		case !n.IsZero() && !s.IsZero() && s.Before(n): // 北,南, 南早於北
			egn.declarer = south
			egn.dummy = north
			leadSeat = west
		case s.IsZero() && !n.IsZero(): //北
			fallthrough
		case !n.IsZero() && !s.IsZero() && n.Before(s): // 北,南, 北早於南
			egn.declarer = north
			egn.dummy = south
			leadSeat = east
		default:
			//TODO: 丟出例外
			slog.Error("GameStartPlayInfo(1)", utilog.Err(errors.New(fmt.Sprintf("最後pass玩家%s, 無法斷定首引,競叫歷史紀錄快取有問題", CbSeat(lastPassSeat)))))
		}

	//東西合約局
	case south, north:
		slog.Info("GameStartPlayInfo", slog.Int("Key", int(east|egn.contractSuit)), slog.String("東叫約時間", fmt.Sprintf("%s", egn.bidHistories[east|egn.contractSuit])))
		slog.Info("GameStartPlayInfo", slog.Int("Key", int(west|egn.contractSuit)), slog.String("西叫約時間", fmt.Sprintf("%s", egn.bidHistories[west|egn.contractSuit])))
		switch e, w := egn.bidHistories[east|egn.contractSuit], egn.bidHistories[west|egn.contractSuit]; {
		case e.IsZero() && !w.IsZero(): //西
			fallthrough
		case !e.IsZero() && !w.IsZero() && w.Before(e): // 東,西, 西早於東
			egn.declarer = west
			egn.dummy = east
			leadSeat = north
		case !e.IsZero() && w.IsZero(): //東
			fallthrough
		case !e.IsZero() && !w.IsZero() && e.Before(w): // 東, 西, 東早於西
			egn.declarer = east
			egn.dummy = west
			leadSeat = south
		default:
			//TODO: 丟出例外
			slog.Error("GameStartPlayInfo(2)", utilog.Err(errors.New(fmt.Sprintf("最後pass玩家%s, 無法斷定首引,競叫歷史紀錄快取有問題", CbSeat(lastPassSeat)))))
		}
	default:
		slog.Error("GameStartPlayInfo", utilog.Err(errors.New(fmt.Sprintf("%d (%s) 不屬於0x%0X, 0x%0X, 0x%0X, 0x%0X", lastPassSeat, CbSeat(lastPassSeat>>6), east, south, west, north))))
	}

	//重要 設定王牌範圍
	egn.trumpRange = TrumpCardRange(egn.Contract)

	egn.Record.contract = CbBid(egn.Contract & valueMark8)

	if !egn.Record.isDouble {
		slog.Debug("GameStartPlayInfo[最終合約]",
			slog.String("FYI",
				fmt.Sprintf("王牌花色: %s  [  %s  ] 最後叫者:%s 首引:%s",
					CbSuit(egn.contractSuit),
					CbBid(egn.Contract&valueMark8),
					CbSeat(egn.Contract&seatMark8),
					CbSeat(leadSeat))))
	} else {
		slog.Debug("GameStartPlayInfo[最終合約]",
			slog.String("FYI",
				fmt.Sprintf("王牌花色: %s  [  %s  ] 最後叫者:%s (原:%s) 賭倍:%s 首引:%s 是否重設lastPassSeat:%t",
					CbSuit(egn.contractSuit),
					egn.Record.contract,
					CbSeat(egn.doubleKeepLastPassPlayer),
					CbSeat(dbgOldPassSeat),
					egn.Record.dbType,
					CbSeat(leadSeat),
					egn.Record.isDouble)))
	}
	return leadSeat, egn.declarer, egn.dummy, egn.contractSuit, *egn.Record, nil
}

// isPassBid 叫品是否是PASS叫品
// memo (DONE)
func (egn *Engine) isZeroBidOrPassBid(value8 uint8) bool {
	// is Zero Bid
	if value8 == zeroBid {
		return true
	}
	// is PASS Bid
	return (value8-uint8(1))%8 == 0
}

func (egn *Engine) StartBid() (nextBidder uint8, limitBiddingValue uint8) {
	egn.contractSuit = valueNotSet
	egn.Contract = valueNotSet
	// 重要  叫品首開叫, 重要: 前端以zeroBid來判斷是不是首叫開始
	//return randomSeat(), zeroBid
	return east, zeroBid
}

func (egn *Engine) IsDoubleBid(bid8 uint8) (dbType CbSuit) {
	dbType = ZeroSuit //注意: 這裡dbType應是CbSuit,但我塞入 CbBid的值 表示沒無db
	switch bid8 {
	case 0x7, 0xF, 0x17, 0x1F, 0x27, 0x2F, 0x37:
		dbType = DOUBLE
	case 0x8, 0x10, 0x18, 0x20, 0x28, 0x30, 0x38:
		dbType = REDOUBLE
	}
	return
}

// GetNextBid 下一輪叫牌, 重要 bidValue 表示(CbSeat | CbBid)組合值
// memo (DONE)
func (egn *Engine) GetNextBid(seat, rawBid8, bidValue uint8) (nextBidder uint8, limitBiddingValue uint8, err error) {
	// seat當前叫者(CbSeat), rrawBid8叫品(CbBid), bidValue (CbSeat | CbBid)組合值
	// nextBidder 下一位叫者, limitBiddingValue下一次禁叫限制

	//當前叫品若不是PASS,則可能會是最後叫品(王牌)產生
	if !egn.isZeroBidOrPassBid(rawBid8) {

		// Double, ReDouble叫品發生
		if db := egn.IsDoubleBid(rawBid8); db != ZeroSuit {
			egn.Record.isDouble = true
			egn.Record.dbType = db

			// yama zorn
			// 發生 double, doubleBidCounter必須+1, 並設定 doubleKeepLastPassPlayer
			egn.doubleBidCounter++
			egn.doubleKeepLastPassPlayer = seat

			//重要: 當 double , redouble 時, 指的是針對之前某一個叫品賭倍,因此不需要紀錄 cacheBidHistories

		} else {

			// 非Double, ReDouble叫品發生

			//重要
			// TODO memo bidValue 是整場遊戲唯一能藉由算出GameResult的重要值
			//  memo 因為bidValue能得這場遊戲是知幾線叫品
			//  memo 因此需要keep 叫到王牌時的rawBid8 直到整場遊戲(GameResult)結果發生
			egn.Contract = bidValue               //帶位置的王牌表示
			err = egn.cacheBidHistories(bidValue) //Zorn
			egn.lastBid = zeroBid                 //暫時設定zeroBid,下面會設回最新的bid

			// yama zorn
			//只要不是 Double 叫品,所有double係相關屬性都歸zero
			egn.Record.isDouble = false
			egn.Record.dbType = ZeroSuit
			egn.doubleBidCounter = 0            //只要不是db叫,counter就歸零
			egn.doubleKeepLastPassPlayer = seat // 只要 doubleBidCounter <= 3時,doubleKeepLastPassPlayer都要被設定
		}

	} else {
		//PASS Bid
		rawBid8 = egn.lastBid //將上一次bid設定給這次的pass bid

		//yama zorn
		// 判斷是否 db 其間,才進行 double計數,並設定 doubleKeepLastPassPlayer
		if egn.Record.isDouble && egn.doubleBidCounter < 3 {
			//只有在counter < 3時 counter += 1, 並設定 lastPlayer
			egn.doubleBidCounter++
			egn.doubleKeepLastPassPlayer = seat
		}
	}

	if !egn.isZeroBidOrPassBid(rawBid8) {
		egn.lastBid = rawBid8
	}

	//底下Lock為了 egn.nextPlay
	//egn.locker.RLock()
	//defer egn.locker.RUnlock()

	return egn.nextPlay, egn.lastBid, err
}

/* ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ */
/*
 ==============================================以下是打牌================================================
  TODO: 底下尚未重購
*/
// playOrder 從四家的出牌,找出第一個出牌者,及另外三家出牌
// 傳入的牌 eastCard, southCard, westCard, northCard 都是不帶位置的
func (egn *Engine) playOrder(eastCard, southCard, westCard, northCard uint8) (first uint8, flowers [3]uint8) {
	/* 首出牌者找出打出哪一張牌
	egn.locker.RLock()
	defer egn.locker.RUnlock()

	switch egn.currentPlay {
	case east:
		first = eastCard
		flowers[0] = southCard
		flowers[1] = westCard
		flowers[2] = northCard
	case south:
		first = southCard
		flowers[0] = eastCard
		flowers[1] = westCard
		flowers[2] = northCard
	case west:
		first = westCard
		flowers[0] = southCard
		flowers[1] = eastCard
		flowers[2] = northCard
	case north:
		first = northCard
		flowers[0] = southCard
		flowers[1] = westCard
		flowers[2] = eastCard
	}
	*/return
}

// playResultInTrump 叫品無王回合比牌
func (egn *Engine) playResultInTrump(eastCard, southCard, westCard, northCard uint8) (winner uint8) {
	/*	var (
			first, flowers = egn.playOrder(eastCard, southCard, westCard, northCard)
			loses          []uint8
			playRange = PlayCardRange(first)
		)

		win := first

		for _, crd := range flowers {
			switch {
			case playRange[0] <= crd && crd <= playRange[1]:
				if crd < win {
					loses = append(loses, crd)
					continue
				}
				loses = append(loses, win)
				win = crd
			default:
				loses = append(loses, crd)
			}
		}

		switch win {
		case eastCard:
			winner = east
		case southCard:
			winner = south
		case westCard:
			winner = west
		case northCard:
			winner = north
		}
	*/
	return uint8(0)
}

// playResultInSuit 叫品王牌回合比牌, 傳入的牌值都是不帶位置的 eastCard,southCard,westCard,northCard
func (egn *Engine) playResultInSuit(eastCard, southCard, westCard, northCard uint8) (winner uint8) {
	/*	var (
			kings []uint8
			loses []uint8
			win   uint8
		)

		for _, card := range [4]uint8{eastCard, southCard, westCard, northCard} {
			if egn.trumpRange[0] <= card && card <= egn.trumpRange[1] {
				kings = append(kings, card)
				continue
			}
			loses = append(loses, card)
		}

		if len(kings) == 1 {
			//只有一張王,勝負已定
			win = kings[0]

		} else if len(kings) > 1 {
			//多張王牌,必須比大小
			win = kings[0]
			for i := 1; i < len(kings); i++ {
				if kings[i] < win {
					loses = append(loses, kings[i])
					continue
				}
				loses = append(loses, win)
				win = kings[i]
			}

		} else {
			//若都沒人出王牌
			var (
				first, flowers = egn.playOrder(eastCard, southCard, westCard, northCard)
				playRange      = PlayCardRange(first)
			)

			//先令win 為首打,在與他牌進行比較
			win = first
			for _, card := range flowers {
				// 必須要與首打同一門
				switch {
				//card必須與首打在同一個區間
				case playRange[0] <= card && card <= playRange[1]:
					if card < win {
						loses = append(loses, card)
						continue
					}
					loses = append(loses, win)
					win = card
				default:
					loses = append(loses, card)
				}
			}
		}
		//找出winner
		switch win {
		case eastCard:
			winner = east
		case southCard:
			winner = south
		case westCard:
			winner = west
		case northCard:
			winner = north
		}
		return
	*/
	return uint8(0)
}

// GetPlayResult winner是本回合贏方,同時也代表是下一位出牌座位(next seat for play)
// 傳入的牌值都是不帶位置的 eastCard,southCard,westCard,northCard
func (egn *Engine) GetPlayResult(eastCard, southCard, westCard, northCard uint8) (winner uint8) {
	/*	switch CbSuit(egn.trumpSuit) {
		case TRUMP:
			winner = egn.playResultInTrump(eastCard, southCard, westCard, northCard)
		default:
			winner = egn.playResultInSuit(eastCard, southCard, westCard, northCard)
		}

		// winner為下一輪首打者
		egn.locker.Lock()
		egn.currentPlay = winner
		egn.locker.Unlock()
		return
	*/
	return uint8(0)
}

// GetGameResult 本局遊戲結果
func (egn *Engine) GetGameResult() {
	//TODO implement me
	panic("implement me")
}
