package game

import (
	"container/ring"
	"errors"
	"log/slog"
	"sync"

	"github.com/moszorn/pb"
)

type (
	seatItem struct {
		Name  *uint8    //代表座位(CbSeat)東南西北,每個SeatItem初始化時必須指派一個不能重覆的位置
		User  *RoomUser //座位上的玩家
		lol   sync.RWMutex
		Value uint8         //當前出的牌(Card)
		Role  pb.SeatStatus //暫沒用到
	}

	SeatManager struct {
		*ring.Ring
		sync.RWMutex
		// action accumulate 表示是否完成一回合.(收到叫牌數,或出牌數,滿4個表示一個回合), 預設值:0
		aa uint8
		//計數已經入座的座位數,當counter == 4 表示遊戲開始
		counter uint8
	}
)

func newSeatManager() *SeatManager {

	r := ring.New(PlayersLimit)
	for i := 0; i < PlayersLimit; i++ {
		r.Value = &seatItem{
			Name: &playerSeats[i],
			Role: pb.SeatStatus_Empty, /*暫沒用到*/
		}
		r = r.Next()
	}
	// ref 此時當前座位(東)
	return &SeatManager{
		Ring: r,
	}
}

// IsGameStart 遊戲是否開始,只要座位坐滿表示遊戲開始
func (mgr *SeatManager) IsGameStart() bool {
	mgr.RLock()
	defer mgr.RUnlock()
	return mgr.counter >= 4
}

func (mgr *SeatManager) IsPlayerOnSeat(player *RoomUser) bool {
	found := false
	limit := PlayersLimit

	var seat *seatItem
	mgr.RLock()

	seat = mgr.Value.(*seatItem)

	// zorn 加
	seat.lol.RLock()
	// zorn 加
	defer seat.lol.RUnlock()

	mgr.RUnlock()

	for limit > 0 && !found {
		limit--
		if seat.User != nil && seat.User == player {
			found = true
			return found
		}
		mgr.Lock()
		mgr.Ring = mgr.Next()
		seat = mgr.Value.(*seatItem)
		mgr.Unlock()
	}
	return found
}

// SetRingSeat 設定玩家入座,玩家離開座位,回傳入座座位,離座座位
// 玩家入座(SeatStatus_SitDown),回傳入座座位seatOn,玩家入座後,isGameStart座位是否已滿牌局開始
// 玩家離座(SeatStatus_StandUp),回傳離座位seatOn,若是回傳的seatOn為valueNotSet表示玩家沒有入座
func (mgr *SeatManager) SetRingSeat(player *RoomUser, set pb.SeatStatus) (seatOn *uint8, isGameStart bool) {
	var seat *seatItem

	//預設result會是 0x0指標,表示(入座,離座)失敗

	mgr.Lock()
	defer mgr.Unlock()
	for i := 0; i < PlayersLimit; i++ {
		seat = mgr.Value.(*seatItem)
		mgr.Ring = mgr.Next()
		switch set {
		case pb.SeatStatus_SitDown:
			//有空位
			// zorn 加
			seat.lol.Lock()
			if seat.User == nil {
				seat.User = player
				seatOn = seat.Name // 入座座位
				//zorn 加
				seat.lol.Unlock()

				player.Tracking = EnterGame
				//player.SeatNo = seat.SeatNo
				mgr.counter++
				return seatOn, mgr.counter >= 4
			}
			//zorn 加
			seat.lol.Unlock()

		case pb.SeatStatus_StandUp:
			//fixme 這裡會有Bug, 因為同樣的玩家有不同的連線登入,所以還需要判斷是否玩家名稱一樣,因為玩家名稱是為一值
			//  if seat.User.Name == player.Name
			//zorn 加
			seat.lol.Lock()
			if seat.User != nil && seat.User == player {
				seat.User = nil
				seatOn = seat.Name //離開座位
				//zorn 加
				seat.lol.Unlock()
				player.Tracking = EnterRoom
				//player.SeatNo = 0
				mgr.counter--
				return seatOn, mgr.counter >= 4
			}
			//zorn 加
			seat.lol.Unlock()
		}
	}
	//注意 有可能搶不到位置,因為遊戲已經開始了
	return seatOn, mgr.counter >= 4
}

// seatShifting 移動Ring座位, 例如:回到東家座位 mgr.seatShifting(east)
// 回傳異動後的下一個座位
func (mgr *SeatManager) seatShifting(seat uint8) *uint8 {
	mgr.Lock()
	defer mgr.Unlock()
	c := mgr.Value.(*seatItem)
	if *c.Name == seat {
		return mgr.Next().Value.(*seatItem).Name
	}

	var nxtSeat *uint8
	for {
		mgr.Ring = mgr.Next()
		c = mgr.Value.(*seatItem)
		if *c.Name == seat {
			nxtSeat = mgr.Next().Value.(*seatItem).Name
			return nxtSeat
		}
	}
}

// 座位找出在ring中對應的位置
func (mgr *SeatManager) seat(seat uint8) *uint8 {
	mgr.RLock()
	c := mgr.Value.(*seatItem)
	mgr.RUnlock()

	c.lol.RLock()
	if *c.Name == seat {
		c.lol.RUnlock()
		return c.Name
	}
	c.lol.RUnlock()

	for {
		mgr.Lock()
		mgr.Ring = mgr.Next()
		mgr.Unlock()

		c = mgr.Value.(*seatItem)
		c.lol.RLock()
		if *c.Name == seat {
			c.lol.RUnlock()
			return c.Name
		}
		c.lol.RUnlock()
	}
}

func (mgr *SeatManager) NsConnBySeat(seat uint8) interface{} {
	mgr.RLock()
	c := mgr.Value.(*seatItem)
	mgr.RUnlock()

	c.lol.RLock()
	if *c.Name == seat && c.User != nil {
		c.lol.RUnlock()
		return c.User.NsConn
	}
	c.lol.RUnlock()

	for {

		//zorn 加
		mgr.Lock()
		mgr.Ring = mgr.Next()
		c = mgr.Value.(*seatItem)
		//zorn 加
		mgr.Unlock()

		c.lol.RLock()
		if *c.Name == seat && c.User != nil {
			c.lol.RUnlock()
			return c.User.NsConn
		}
		c.lol.RUnlock()
	}
}

// setSeatValue 依照座位存放出牌(不含seat的出牌)
func (mgr *SeatManager) setSeatValue(seat, value uint8) (setSuccess bool) {
	mgr.RLock()
	c := mgr.Value.(*seatItem)
	mgr.RUnlock()

	c.lol.RLock()
	if *c.Name == seat {
		c.lol.RUnlock()

		c.lol.Lock()
		c.Value = value
		c.lol.Unlock()

		return true
	}

	c.lol.RUnlock()
	for {
		mgr.Lock()
		mgr.Ring = mgr.Next()
		c = mgr.Value.(*seatItem)
		mgr.Unlock()

		c.lol.RLock()
		if *c.Name == seat {
			c.lol.RUnlock()

			c.lol.Lock()
			c.Value = value
			c.lol.Unlock()
			return true
		}
		c.lol.RUnlock()
	}
}

// 這個方法是舊有的 seatPlays改版, 回傳座位上玩家資訊 (可能座位上會沒人nil)
func (mgr *SeatManager) seatPlays() (eastPlay, southPlay, westPlay, northPlay *RoomUser) {
	mgr.Do(func(i any) {
		v := i.(*seatItem)
		switch *v.Name {
		case uint8(east):
			eastPlay = v.User
		case uint8(south):
			southPlay = v.User
		case uint8(west):
			westPlay = v.User
		case uint8(north):
			northPlay = v.User
		}
	})
	return
}

// playSeat 出牌者, playValue 打出什麼牌, 回傳是否這次出牌已經滿四人出牌,可進入下一回合了
func (mgr *SeatManager) seatPlay(playSeat, playValue uint8) (roundCompleted bool) {
	//step1. shift到 playSeat位置, 設定SeatItem.play
	//step2. 累加 aa

	if mgr.aa >= 4 {
		err := errors.New("已滿四人出牌,無法設定seat value")
		slog.Error("seatPlay", slog.String(".", err.Error()))
	}

	//step1 設定seat value(card)
	if !mgr.setSeatValue(playSeat, playValue) {
		err := errors.New("設定座位出牌出錯!!")
		slog.Error("seatPlay", slog.String(".", err.Error()))
	}

	//step2. 出牌計數 +1 累計
	mgr.aa++

	//step3.
	//if aa >= 4 表示已經一回合,回傳true,讓Game令engine進行結算
	//if aa < 4 表示回合尚未完整回傳false
	return mgr.aa >= 4
}

func (mgr *SeatManager) resetPlay() {
	mgr.aa = 0x0
	mgr.Do(func(i any) {
		v := i.(*seatItem)
		v.Value = valueNotSet
	})
}
