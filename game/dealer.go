package game

import (
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
)

const (
	//NumOfCardsInDeck : There are 52 cards in a standard deck of cards	一副牌將張撲克
	NumOfCardsInDeck int = 52
	// NumOfCardsOnePlayer 一局遊戲每位玩家最多持牌
	NumOfCardsOnePlayer int = 13

	FirstLeadSignal uint8 = 0
)

// 發牌前排序玩家手(hand)的牌
func sortHand(hand []*uint8) {
	sort.Slice(hand, func(i, j int) bool {
		return *hand[i] < *hand[j]
	})
}

// 對 Game's的deck進行洗牌
func shuffle(deck *[NumOfCardsInDeck]*uint8) {
	//因為使用了random,所以必須設定一個rand seed否則每次洗牌結果都是一樣
	rand.Shuffle(NumOfCardsInDeck, func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
}

// inPlaySync 同步各家持牌
func inPlaySync(g *Game) {
	for idx, s := range playerSeats {
		//hand := [13]uint8{}
		cards := g.Deck[&playerSeats[idx]]
		for i := range cards {
			//	hand[i] = *cards[i]

			g.deckInPlay[s][i] = *cards[i]
		}
		//g.deckInPlay[s] = hand
	}
	/*....................................................................*/

	//底下只是為了Debug用, 可以移掉
	var cards []string
	for idx := range playerSeats {
		uint8s := g.deckInPlay[playerSeats[idx]]
		cards = make([]string, 0, len(uint8s))
		for i := 0; i < len(uint8s); i++ {
			cards = append(cards, fmt.Sprintf("%s", CbCard(uint8s[i])))
		}
		slog.Debug("inPlaySync初始牌分配",
			slog.String("座位", fmt.Sprintf("%s", CbSeat(playerSeats[idx]))),
			slog.String("牌", strings.Join(cards, "  ")))
	}
}

// Shuffle Game開始前的洗牌,並同步Game的deckInPlay
func Shuffle(g *Game) {
	shuffle(&g.deck)
	for seatPtr := range g.Deck {
		sortHand(g.Deck[seatPtr])
	}
	inPlaySync(g)
}

// NewDeck Game初始前必須設定一副牌,並化分為四個區域(東南西北座位)
func NewDeck(g *Game) {

	//以東南西北座位每13個切分g.deck成為 g.Deck
	g.Deck = map[*uint8][]*uint8{
		&playerSeats[0]: g.deck[:13:13],
		&playerSeats[1]: g.deck[13 : 13+13 : 13+13],
		&playerSeats[2]: g.deck[13+13 : 13+13+13 : 13+13+13],
		&playerSeats[3]: g.deck[13+13+13 : 52 : 52],
	}

	//填牌:將代表牌值的常數指標,存到g.deck,這樣不論有幾局,就只用一副牌
	// 執行後 g.Deck有就完成每桌的牌
	for i := 0; i < NumOfCardsInDeck; i++ {
		g.deck[i] = &deck[i]
	}

	//deckInPlay是遊戲進行時各家持牌狀態,它是由g.Deck clone過去的
	g.deckInPlay = make(map[uint8]*[13]uint8)
	for idx := range playerSeats {
		cards := g.Deck[&playerSeats[idx]]
		hand := [13]uint8{}
		for i := range cards {
			hand[i] = *cards[i]
		}
		g.deckInPlay[playerSeats[idx]] = &hand
	}
	/*....................................................................*/
	//底下只是為了Debug用, 可以移掉
	var cards []string
	for idx := range playerSeats {
		uint8s := g.deckInPlay[playerSeats[idx]]
		cards = make([]string, 0, len(uint8s))
		for i := 0; i < len(uint8s); i++ {
			cards = append(cards, fmt.Sprintf("%s", CbCard(uint8s[i])))
		}
		slog.Debug("NewDeck初始牌分配",
			slog.String("座位", fmt.Sprintf("%s", CbSeat(playerSeats[idx]))),
			slog.String("牌", strings.Join(cards, "  ")))
	}
}
