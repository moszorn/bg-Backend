package game

import (
	"errors"
	"fmt"
	"time"
)

type (
	DoubleButton struct {
		value uint8 //Double CbBid值
		isOn  uint8 // 1: On \ 0:Off
	}

	//代表一個競叫
	bidItem struct {
		t      time.Time // 前端叫約時間,此屬性用以找尋先叫合約的人
		bidder CbSeat    // 競叫者
		value  CbBid     // 叫品(不帶位置)

		dbType CbSuit //叫品屬於哪類Double(只限DOUBLE, REDOUBLE,預設值ZeroSuit表未設定)
		b      uint8  //CbSeat | CbBid
	}

	bidHistory struct {
		h         []*bidItem          // 競叫紀錄
		histories map[uint8]time.Time // 競叫者(CbSeat)與所叫王牌花色(Suit)聯合為KEY, Value是競叫者叫約時間(即bidItem.t)
	}
)

func createBidItem(seat, bid uint8) *bidItem {

	b := &bidItem{
		t:      time.Now(),
		bidder: CbSeat(seat),
		value:  CbBid(bid),
		b:      seat | bid,
	}

	switch bid {
	case uint8(Db1), uint8(Db2), uint8(Db3), uint8(Db4), uint8(Db5), uint8(Db6), uint8(Db7):
		b.dbType = DOUBLE
	case uint8(Db1x2), uint8(Db2x2), uint8(Db3x2), uint8(Db4x2), uint8(Db5x2), uint8(Db6x2), uint8(Db7x2):
		b.dbType = REDOUBLE
	default:
		b.dbType = ZeroSuit
	}
	return b
}

// 競叫者
func (b *bidItem) who() uint8 {
	return b.b & seatMark8
	// 或
	//return uint8(b.bidder)
}

// 競叫品
func (b *bidItem) bid() uint8 {
	return b.b & valueMark8
	// 或
	//return uint8(b.bid)
}

// 叫品是否是PASS叫
func (b *bidItem) isPass() bool {
	// is Zero Bid
	if b.value == BidYet {
		return true
	}

	// is PASS Bid
	return (b.b-uint8(1))%8 == 0

	//return ((b.b & valueMark8) - uint8(1))%8 == 0
}

// 叫品是否為 Double/Double x2叫
func (b *bidItem) isDouble() bool {
	switch b.b & valueMark8 {
	case uint8(Db1), uint8(Db2), uint8(Db3), uint8(Db4), uint8(Db5), uint8(Db6), uint8(Db7):
		return true
	case uint8(Db1x2), uint8(Db2x2), uint8(Db3x2), uint8(Db4x2), uint8(Db5x2), uint8(Db6x2), uint8(Db7x2):
		return true
	}
	return false
}

// 關鍵叫,競叫品不是valueNotSet,也不是PASS
func (b *bidItem) isCrucial() bool {
	return !b.isPass() && (b.b&valueMark8) != valueNotSet
}

// *----------------------
func createBidHistory() *bidHistory {
	return &bidHistory{
		h:         make([]*bidItem, 0, 56),
		histories: make(map[uint8]time.Time),
	}
}

// LastBid 上一個不是PASS的叫品(或稱最新叫品)
func (h *bidHistory) LastBid() uint8 {
	var (
		l         = len(h.h)
		shift int = 1
	)
	if l < 1 {
		return uint8(Pass1)
	}
	for ; shift <= l; shift++ {
		//濾除PASS,濾除Doubl 找出最新一個不是PASS的叫品
		if h.h[l-shift].isCrucial() && !h.h[l-shift].isDouble() { //濾掉所有pass
			return h.h[l-shift].bid()
		}
	}
	return uint8(Pass1)
}

// Bid 叫牌,並且存入叫牌紀錄
func (h *bidHistory) Bid(seat, bid uint8) *bidItem {

	//TODO check seat , bid valid

	b := createBidItem(seat, bid)
	h.h = append(h.h, b)

	var (
		cbSuit, ok = seatBiddingMapperSuit[seat|bid]

		// 重要 SbSeat合併SbSuit 才是歷史紀錄
		token = seat | cbSuit
	)
	if _, ok = h.histories[token]; !ok {
		//以座位儲存叫牌紀錄,並附上時搓,到時以時搓比對是哪一座位先以此花色為王,他就是莊
		//slog.Debug("Bid", slog.String("FYI", fmt.Sprintf("token(%03d) [%s0x%02x | 0x%02x %-10s] %s\n", token, CbSeat(seat), seat, cbSuit, CbSuit(cbSuit), b.t.Format("2006-01-02 15:04:05.000"))))
		h.histories[token] = b.t
	}
	return b
}

// Clear 清空集合項目,但記憶體仍保留
func (h *bidHistory) Clear() {
	h.h = h.h[:0]
	clear(h.histories)

	//TODO 清除
}

// 叫牌結束, 檢查最後三個叫品,都是PASS就是叫牌結束 注意:執行isBidOver不能執行 reverse
// 是否重新洗牌

// IsBidFinishedOrReBid 是否叫牌結束(done),是否重新洗牌(reBid),或競叫結束(reBid)
func (h *bidHistory) IsBidFinishedOrReBid() (bidComplete bool, needReBid bool) {
	l := len(h.h)
	if l < 4 {
		//不到一輪競叫,表示仍在競叫中
		return
	}

	last1, last2, last3, last4 := h.h[l-1], h.h[l-2], h.h[l-3], h.h[l-4]

	//done 表示最後三個競叫一定是PASS, 倒數第四個一定是關鍵叫
	bidComplete = (last1.isPass() && last2.isPass() && last3.isPass() && last4.isCrucial()) || (last1.isPass() && last2.isPass() && last3.isPass() && last4.isDouble()) || (last1.isPass() && last2.isPass() && last3.isPass() && last4.isPass())
	// 重新競叫,最後四個叫品一定是PASS
	needReBid = last1.isPass() && last2.isPass() && last3.isPass() && last4.isPass()
	return
}

// GameStartPlayInfo 只有當IsLastBidOrReBid回傳(done為true, reBid為false)這個方法才有意義,回傳首引,莊家,夢家,合約王花色, 合約計分
func (h *bidHistory) GameStartPlayInfo() (lead, declarer, dummy, contractSuit uint8, biddingResult record, err error) {

	if bidComplete, needReBid := h.IsBidFinishedOrReBid(); !bidComplete || (bidComplete && needReBid) {
		return seatYet, seatYet, seatYet, seatYet, record{}, errors.New("競叫未完成")
	}

	biddingResult = record{
		isDouble: false,
		dbType:   ZeroSuit, /*dbType 預設值使前端不顯示任何字像*/
		contract: 0,        /**/
	}

	var (
		contract *bidItem
		shift    int = 1
		l            = len(h.h)
	)
	//從最後一個item往前找
	for ; shift <= l; shift++ {
		if h.h[l-shift].isCrucial() { //濾掉所有pass

			contract = h.h[l-shift]
			//bid不是Double就是Contract
			if contract.isDouble() {
				biddingResult.isDouble = contract.isDouble()
				biddingResult.dbType = contract.dbType
				// ..................................
			} else {
				//找到有效叫品, contract 合約確定
				break
			}
		}
	}

	//重要 取得合約王牌花色(Suit), 並設定合約
	contractSuit = seatBiddingMapperSuit[contract.b]
	biddingResult.contract = contract.value

	// 將叫到合約方(兩家)進行判斷,看誰先叫的合約
	partner, lead1 := partnerOppositionLead(contract.bidder)
	_, lead2 := partnerOppositionLead(CbSeat(partner))

	token1, token2 := contract.who()|contractSuit, partner|contractSuit

	//fmt.Printf(" token1(%s | %s)%s\n token2 (%s | %s)%s\n", CbSeat(contract.who()), CbSuit(contractSuit), h.histories[token1].Format("15:04:05"), CbSeat(partner), CbSuit(contractSuit), h.histories[token2].Format("15:04:05"))
	switch t1, t2 := h.histories[token1], h.histories[token2]; {
	case !t1.IsZero() && t2.IsZero():
		fallthrough
	case !t2.IsZero() && !t1.IsZero() && t1.Before(t2):
		lead = lead1
		declarer = contract.who()
		dummy = partner
	case t1.IsZero() && !t2.IsZero():
		fallthrough
	case !t2.IsZero() && !t1.IsZero() && t2.Before(t1):
		lead = lead2
		declarer = partner
		dummy = contract.who()
	}
	fmt.Printf("GameStartPlayInfo: bidder:%s bid:%s isDb:%t\n", contract.bidder, contract.value, contract.isDouble())
	return
}

// partnerOppositionLead 取得傳入座(who)隊友,與敵方首引
func partnerOppositionLead(who CbSeat) (partner, lead uint8) {
	switch who {
	case east:
		return uint8(west), uint8(south)
	case south:
		return uint8(north), uint8(west)
	case west:
		return uint8(east), uint8(north)
	case north:
		return uint8(south), uint8(east)
	default:
		return uint8(seatYet), uint8(seatYet)
	}
}

// 依照叫品回傳該線位Double與Double x2 (例如:傳入 4C, 就會還傳 Db4 與 Db4x2

// GetDoubleAtSameLine 取得同線位Double,ReDouble
func GetDoubleAtSameLine(bid8 uint8) (uint8, uint8) {
	var (
		double   CbBid
		redouble CbBid
	)
	switch cbBid := CbBid(bid8); {
	case cbBid < Pass2: /* 線位1 */
		double = Db1
		redouble = Db1x2
	case cbBid >= C2 && cbBid < Pass3: /* 線位 2*/
		double = Db2
		redouble = Db2x2
	case cbBid >= C3 && cbBid < Pass4: /* 線位 3*/
		double = Db3
		redouble = Db3x2
	case cbBid >= C4 && cbBid < Pass5: /* 線位 4*/
		double = Db4
		redouble = Db4x2
	case cbBid >= C5 && cbBid < Pass6: /* 線位 5*/
		double = Db5
		redouble = Db5x2
	case cbBid >= C6 && cbBid < Pass7: /* 線位 6*/
		double = Db6
		redouble = Db6x2
	case cbBid >= C7: /* 線位 7*/
		double = Db7
		redouble = Db7x2
	}
	return uint8(double), uint8(redouble)
}
