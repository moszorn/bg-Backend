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

	//lastBid 表示上一次叫品,其初始叫品為 0x1 (參考7線叫品值),叫牌過程中,當前叫品絕對不會小於或等於lastBid,除了 PASS bid外
	lastBid uint8

	//四家Suit叫牌時間紀錄, Key: CbSeat | CbSuit , value: 時間(用來比對玩家叫品先後順序,以找出莊家)
	bidHistories map[uint8]time.Time

	// 重要 這個屬性(CbSeat + CbBid) 為競叫屬性,代表叫到幾線,哪一方叫到王, 是唯一一處知道幾線叫品的屬性
	/*
		如何從 trumpInCompetitiveBidding 知道是誰叫到合約,合約又是什麼?
			因為 trumpInCompetitiveBidding 由 CbSeat | SbSuit
		所以 seat = trumpInCompetitiveBidding & 0xF0
			合約  = trumpInCompetitiveBidding & 0x00
	*/
	//遊戲結算階段 trumpInCompetitiveBidding, GameResult是透過這個屬性才能結算
	//  表示該局遊戲最終叫品
	trumpInCompetitiveBidding uint8 //注意 trumpInCompetitiveBidding 是CbSeat + CbBid

	//遊戲王牌合約 (game.CLUB,game.DIAMOND,game.HEART,game.SPADE,game.TRUMP)
	contract uint8 //注意 trumpSuit 是CbSuit

	//王張區間, 在計算首引(getGameFirstLead)時計算出王張區間,代表本局合法的王牌有哪幾張
	trumpRange CardRange

	// 表示三家pass,一家有叫品(trumpInCompetitiveBidding),表示叫到王牌競叫結束
	// 未來 當遊戲桌關閉時,記得一同關閉channel 以免leaking 重要
	passBuffered chan struct{}

	//本局莊家,計算GameResult會用到
	declarer uint8
	//本局夢家,計算GameResult會用到
	dummy uint8

	//.................................................)

	//表示當前叫牌玩家,或出牌玩家座位
	currentPlay uint8
	//表示下一個叫牌,下一個出牌者座位
	nextPlay uint8
}

func newEngine() *Engine {

	egn := &Engine{
		passBuffered:              make(chan struct{}, 4),
		bidHistories:              make(map[uint8]time.Time),
		trumpRange:                CardRange{},
		lastBid:                   0x1,
		trumpInCompetitiveBidding: valueNotSet,
		contract:                  valueNotSet,
		declarer:                  valueNotSet,
		dummy:                     valueNotSet,
		currentPlay:               valueNotSet,
		nextPlay:                  valueNotSet,
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

// ClearBiddingState 競叫底定,或準備重新競叫前執行清除競叫紀錄
// memo DONE
func (egn *Engine) ClearBiddingState() {
	clear(egn.bidHistories)
	//初始叫品為 0x1 (參考7線叫品值)
	egn.lastBid = 0x1
	egn.currentPlay = valueNotSet
	egn.nextPlay = valueNotSet
	egn.trumpInCompetitiveBidding = valueNotSet
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

	// 此次叫品為PASS
	egn.passBuffered <- struct{}{}

	if len(egn.passBuffered) <= 3 && egn.trumpInCompetitiveBidding == valueNotSet /* PASS叫品未滿,競叫仍未底定*/ {
		return
	} else if len(egn.passBuffered) == 3 && !egn.isZeroBidOrPassBid(egn.trumpInCompetitiveBidding&valueMark8) /*PASS已滿三個,且競叫底定*/ {
		//競叫底定
		bidCompleted = true
		//清空 passBuffered
		drainBuffer()
		return
	}
	//四人競叫,結果競叫流標
	if len(egn.passBuffered) == 4 {
		drainBuffer()
		bidCompleted = true
		bidReDo = true
	}
	return
}

// cacheBidHistories 將以座位叫的叫品以( CbSeat | CbSuit )形式儲存保留便於後續決定王牌
// memo DONE
func (egn *Engine) cacheBidHistories(seat8, cbSeatCbBid uint8) error {
	//seat8 叫者
	//cbSeatCbBid叫品(CbSeat+CbBid)找出對應的CbSuit
	cbSuit, ok := rawBidSuitMapper[cbSeatCbBid]
	if !ok {
		//TODO 這裡發生不知名叫品
		return ErrUnknownBid
	}
	// seat 結合 suit作為儲存記錄項目
	history := seat8 | cbSuit
	if _, ok = egn.bidHistories[cbSuit]; !ok {
		//以座位儲存叫牌紀錄,並附上時搓,到時以時搓比對是哪一座位先叫此花色為王,他就是莊
		egn.bidHistories[history] = time.Now()
	}
	return nil
}

// GameStartPlayInfo 競叫結束,以最後叫pass座位為參數取得 首引,莊家,夢家,合約, 幾線合約
// memo DONE 取代原 getGameFirstLead
func (egn *Engine) GameStartPlayInfo(lastPassSeat uint8) (leadSeat, declarerSeat, dummySeat, contract, lineContract uint8, err error) {
	// seatForFinalPASS 參數:最後一個叫PASS的座位
	var ok bool
	egn.contract = valueNotSet

	if egn.trumpInCompetitiveBidding == valueNotSet {
		return valueNotSet, valueNotSet, valueNotSet, valueNotSet, valueNotSet, ErrUnContract
	}

	egn.contract, ok = rawBidSuitMapper[egn.trumpInCompetitiveBidding]

	if !ok {
		slog.Error("GameStartPlayInfo", slog.String("FYI", fmt.Sprintf("CbSeat(%s), CbBid(%s)無法對應任何叫品", CbSeat(egn.trumpInCompetitiveBidding&seatMark8), CbBid(egn.trumpInCompetitiveBidding&valueMark8))), utilog.Err(ErrUnContract))
		return valueNotSet, valueNotSet, valueNotSet, valueNotSet, valueNotSet, ErrUnContract
	}

	//lastPassSeat = lastPassSeat << 6
	switch lastPassSeat {
	//南北合約局
	case east, west: //最後PASS的是 east, west
		switch s, n := egn.bidHistories[south|egn.contract], egn.bidHistories[north|egn.contract]; {
		case !n.IsZero() && s.IsZero():
			fallthrough
		case !n.IsZero() && !s.IsZero() && s.Before(n):
			egn.declarer = south
			egn.dummy = north
			leadSeat = west
		case !s.IsZero() && n.IsZero():
			fallthrough
		case !n.IsZero() && !s.IsZero() && n.Before(s):
			egn.declarer = north
			egn.dummy = south
			leadSeat = east
		default:
			//TODO: 丟出例外
			slog.Error("GameStartPlayInfo(1)", utilog.Err(errors.New(fmt.Sprintf("最後pass玩家%s, 無法斷定首引,競叫歷史紀錄快取有問題", CbSeat(lastPassSeat)))))
		}

	//東西合約局
	case south, north:
		switch e, w := egn.bidHistories[east|egn.contract], egn.bidHistories[west|egn.contract]; {
		case e.IsZero() && !w.IsZero():
			fallthrough
		case !e.IsZero() && !w.IsZero() && w.Before(e):
			egn.declarer = west
			egn.dummy = east
			leadSeat = north
		case !e.IsZero() && w.IsZero():
			fallthrough
		case !e.IsZero() && !w.IsZero() && e.Before(w):
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
	egn.trumpRange = TrumpCardRange(egn.trumpInCompetitiveBidding)

	//slog.Debug("GameStartPlayInfo============>", slog.String("最終合約", fmt.Sprintf("Contract: %s [  %s  ] 首引:%s 莊家:%s", CbSuit(egn.contract), CbBid(egn.trumpInCompetitiveBidding), CbSeat(leadSeat), CbSeat(declarerSeat))))
	return leadSeat, egn.declarer, egn.dummy, egn.contract, egn.trumpInCompetitiveBidding, nil
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
	egn.contract = valueNotSet
	egn.trumpInCompetitiveBidding = valueNotSet
	// 重要  叫品首開叫, 重要: 前端以zeroBid來判斷是不是首叫開始
	//return randomSeat(), zeroBid
	return east, zeroBid
}

// GetNextBid 下一輪叫牌, 重要 bidValue 表示(CbSeat | CbBid)組合值
// memo (DONE)
func (egn *Engine) GetNextBid(seat, rawBid8, bidValue uint8) (nextBidder uint8, limitBiddingValue uint8, err error) {
	// seat當前叫者(CbSeat), rrawBid8叫品(CbBid), bidValue (CbSeat | CbBid)組合值
	// nextBidder 下一位叫者, limitBiddingValue下一次禁叫限制

	//當前叫品若不是PASS,則可能會是最後叫品(王牌)產生
	if !egn.isZeroBidOrPassBid(rawBid8) {
		//重要
		// TODO memo bidValue 是整場遊戲唯一能藉由算出GameResult的重要值
		//  memo 因為bidValue能得這場遊戲是知幾線叫品
		//  memo 因此需要keep 叫到王牌時的rawBid8 直到整場遊戲(GameResult)結果發生
		egn.trumpInCompetitiveBidding = bidValue    //帶位置的王牌表示
		err = egn.cacheBidHistories(seat, bidValue) //Zorn
		egn.lastBid = zeroBid                       //暫時設定zeroBid,下面會設回最新的bid

	} else {
		//PASS Bid
		rawBid8 = egn.lastBid
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
