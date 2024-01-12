package game

import (
	"errors"
	"fmt"
	"log/slog"
	"math/rand"

	utilog "github.com/moszorn/utils/log"
)

// 隨機開叫產出 TODO:
func randomSeat() uint8 {
	return playerSeats[rand.Int31n(4)]
}

// isPassBid 叫品是否是PASS叫品
func isZeroBidOrPassBid(value8 uint8) bool {
	// is Zero Bid
	if value8 == uint8(BidYet) {
		return true
	}
	// is PASS Bid
	return (value8-uint8(1))%8 == 0
}

type Engine struct {
	//locker sync.RWMutex

	bidHistory *bidHistory

	//底下三個在競叫底定,遊戲開始前 SetGamePlayInfo 設定
	trumpRange CardRange //王張區間,首引
	declarer   CbSeat    //本局莊家,計算GameResult會用到
	dummy      CbSeat    //本局夢家,計算GameResult會用到
	//.................................................

	//表示當前叫牌玩家,或出牌玩家座位
	currentPlay uint8
}

func newEngine() *Engine {

	egn := &Engine{
		bidHistory:  createBidHistory(),
		trumpRange:  CardRange{},
		declarer:    seatYet,
		dummy:       seatYet,
		currentPlay: valueNotSet,
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

// ClearGameState Engine 狀態還原
func (egn *Engine) ClearGameState() {}

// ClearBiddingState 競叫底定,四家PASS 或準備重新競叫前執行清除競叫紀錄
// memo DONE
func (egn *Engine) ClearBiddingState() {
	egn.bidHistory.Clear()
}

// GameStartPlayInfo 競叫結束,以最後叫pass的玩家座位(lastPassSeat)為參數取得 leasSeat首引, declarerSeat莊家, dummySeat夢家, contractSuit王牌花, contract 合約紀錄(包含是否db,叫品線位)
func (egn *Engine) GameStartPlayInfo() (lead, declarer, dummy, suit uint8, contract record, err error) {

	lead, declarer, dummy, suit, contract, err = egn.bidHistory.GameStartPlayInfo()
	if err != nil {
		slog.Warn("GameStartPlayInfo", utilog.Err(err))
		return lead, declarer, dummy, suit, contract, err
	}

	//重要: 設定引擎王牌
	egn.trumpRange = GetTrumpRange(suit)
	egn.currentPlay = lead
	egn.declarer = CbSeat(declarer)
	egn.dummy = CbSeat(dummy)

	return lead, declarer, dummy, suit, contract, nil
}

func (egn *Engine) IsBidFinishedOrReBid() (bidComplete bool, needReBid bool) {
	return egn.bidHistory.IsBidFinishedOrReBid()
}

// StartBid 初始競叫開始
func (egn *Engine) StartBid() (nextBidder uint8, limitBiddingValue uint8) {
	// 重要  叫品首開叫, 重要: 前端以zeroBid來判斷是不是首叫開始
	//return randomSeat(), zeroBid
	return uint8(east), uint8(BidYet)
}

func (egn *Engine) GetNextBid(seat, bid uint8) (nextBiddingLimit uint8, db DoubleButton, db2 DoubleButton) {
	bidding := egn.bidHistory.Bid(seat, bid)

	nextBiddingLimit = egn.bidHistory.LastBid()
	db = DoubleButton{}
	db2 = DoubleButton{}

	if !bidding.isCrucial() {
		//PASS跳過
		return
	} else {
		db.value, db2.value = GetDoubleAtSameLine(bid)
		switch bidding.isDouble() {
		case true:
			//前端叫double,下一個叫者就要關閉double,顯示redouble
			//前端叫double x2,下一個叫者兩個double選項都要關閉,只能往下一線叫
			switch bidding.dbType {
			case DOUBLE:
				db2.isOn = true
			}
		case false:
			//前端叫正常叫品,下一個叫者就要要顯示double
			db.isOn = true
		}
	}
	/*
		slog.Debug("GetNextBid",
			slog.String(fmt.Sprintf("%s", bidding.bidder), fmt.Sprintf(" %s ", bidding.value)),
			slog.String("FYI", fmt.Sprintf("X:%t  Xtype:%s  isPass:%t", bidding.isDouble(), bidding.dbType, bidding.isPass())),
			slog.String("FYI", fmt.Sprintf("db:%t  db2:%t", db.isOn == 1, db2.isOn == 1))) */

	return
}

/* ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ ♣️♦️♥️♠️ */

// playOrder 從四家的出牌,找出第一個出牌者,及另外三家出牌
// 傳入的牌 eastCard, southCard, westCard, northCard 都是不帶位置的
func (egn *Engine) playOrder(eastCard, southCard, westCard, northCard uint8) (firstPlay uint8, flowerPlays [3]uint8) {
	// 首出牌者找出打出哪一張牌
	//egn.locker.RLock()
	//defer egn.locker.RUnlock()

	/*	switch CbSeat(egn.currentPlay) {
		case east:
			firstPlay = eastCard
			flowerPlays[0] = southCard
			flowerPlays[1] = westCard
			flowerPlays[2] = northCard
		case south:
			firstPlay = southCard
			flowerPlays[0] = eastCard
			flowerPlays[1] = westCard
			flowerPlays[2] = northCard
		case west:
			firstPlay = westCard
			flowerPlays[0] = southCard
			flowerPlays[1] = eastCard
			flowerPlays[2] = northCard
		case north:
			firstPlay = northCard
			flowerPlays[0] = southCard
			flowerPlays[1] = westCard
			flowerPlays[2] = eastCard
		}
	*/

	switch CbSeat(egn.currentPlay) {
	case east:
		firstPlay = southCard
		flowerPlays[0] = westCard
		flowerPlays[1] = northCard
		flowerPlays[2] = eastCard
	case south:
		firstPlay = westCard
		flowerPlays[0] = northCard
		flowerPlays[1] = eastCard
		flowerPlays[2] = southCard
	case west:
		firstPlay = northCard
		flowerPlays[0] = eastCard
		flowerPlays[1] = southCard
		flowerPlays[2] = westCard
	case north:
		firstPlay = eastCard
		flowerPlays[0] = southCard
		flowerPlays[1] = westCard
		flowerPlays[2] = northCard
	}
	return
}

// playResultInTrump 叫品無王回合比牌
func (egn *Engine) playResultInTrump(eastCard, southCard, westCard, northCard uint8) (winner uint8) {
	var (
		first, flowers = egn.playOrder(eastCard, southCard, westCard, northCard)
		loses          []uint8
		playRange      = GetRoundRangeByFirstPlay(first)
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
		winner = uint8(east)
	case southCard:
		winner = uint8(south)
	case westCard:
		winner = uint8(west)
	case northCard:
		winner = uint8(north)
	}

	slog.Warn("無王回合比較大小",
		utilog.Err(errors.New(
			fmt.Sprint(fmt.Sprintf("min:%s ~ max: %s", CbCard(playRange[0]), CbCard(playRange[1])),
				fmt.Sprintf("首打: %s ,  東: %s 南: %s  西: %s  北: %s  , 最後誰贏:%s",
					CbCard(first),
					CbCard(eastCard),
					CbCard(southCard),
					CbCard(westCard),
					CbCard(northCard),
					CbSeat(winner))))))

	return winner
}

// playResultInSuit 叫品王牌回合比牌, 傳入的牌值都是不帶位置的 eastCard,southCard,westCard,northCard
func (egn *Engine) playResultInSuit(eastCard, southCard, westCard, northCard uint8) (winner uint8) {
	var (
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
			playRange      = GetRoundRangeByFirstPlay(first)
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
		winner = uint8(east)
	case southCard:
		winner = uint8(south)
	case westCard:
		winner = uint8(west)
	case northCard:
		winner = uint8(north)
	}

	slog.Warn("有王回合比較大小",
		utilog.Err(errors.New(
			fmt.Sprint(fmt.Sprintf("王牌範圍 %s ~ %s ", CbCard(egn.trumpRange[0]), CbCard(egn.trumpRange[1])),
				fmt.Sprintf("東: %s 南: %s  西: %s  北: %s  , 最後誰贏:%s",
					CbCard(eastCard),
					CbCard(southCard),
					CbCard(westCard),
					CbCard(northCard),
					CbSeat(winner))))))
	return
}

// GetPlayResult winner是本回合贏方,同時也代表是下一位出牌座位(next seat for play)
// 傳入的牌值都是不帶位置的 eastCard,southCard,westCard,northCard
func (egn *Engine) GetPlayResult(eastCard, southCard, westCard, northCard uint8, gameSuit CbSuit) (winner uint8) {

	switch gameSuit {
	case TRUMP:
		winner = egn.playResultInTrump(eastCard, southCard, westCard, northCard)
	default:
		winner = egn.playResultInSuit(eastCard, southCard, westCard, northCard)
	}

	slog.Warn("回合結果", utilog.Err(errors.New(fmt.Sprint("FYI", fmt.Sprintf("王牌:%s  東: %s 南: %s  西: %s  北: %s  , 最後誰贏:%s", gameSuit, CbCard(eastCard), CbCard(southCard), CbCard(westCard), CbCard(northCard), CbSeat(winner))))))

	// winner為下一輪首打者
	//egn.locker.Lock()
	egn.currentPlay = winner
	//egn.locker.Unlock()
	return
}

// IsTrumpCard 牌(card)是否是該局王牌
func (egn *Engine) IsTrumpCard(card uint8) bool {
	return egn.trumpRange[0] <= card && card <= egn.trumpRange[1]
}

// GetCardSound 回傳此牌(card)的音效 (王牌音效字串或一般音效字串)
func (egn *Engine) GetCardSound(card uint8) string {
	//參考前端: get_asset.dart - SoundAsset
	if egn.IsTrumpCard(card) {
		return "card-trump"
	}
	return "card-play"
}

// GetGameResult 本局遊戲結果
func (egn *Engine) GetGameResult() {
	//TODO implement me
	panic("implement me")
}
