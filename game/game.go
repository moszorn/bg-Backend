package game

import (
	"fmt"

	"github.com/moszorn/pb"
	"github.com/moszorn/utils/rchanr"
	"github.com/moszorn/utils/skf"
)


var "未完成"

// 進入遊戲房間的連線
func NewUser(conn *skf.NSConn) *RoomUser {
	return &RoomUser{
		NsConn:   conn,
		Name:     "",
		Tracking: 0,
	}
}


type PayloadType uint8

const (
	ByteType PayloadType = iota
	ProtobufType
)

// payloadData 代表資料送到指定Client
type payloadData struct {
	Player    uint8      //代表player seat 通常針對指定的玩家, 表示Zone的情境應該不會發生
	Data      []uint8    // 可以是byte, bytes
	ProtoData pb.Message // proto
	PayloadType PayloadType //這個 payload 屬於那種型態的封	包
}


// CreateCBGame 建立橋牌(Contract Bridge) Game
func CreateCBGame(tableName string, tableId int32) *Game {

	//todo
	e := newEngine()

	cbGame := &Game{
		//gateway:      make(chan rchanr.ChanRepWithArguments[*RoomUser, *chanResult]),
		//findUser:     make(chan rchanr.ChanRepWithArguments[*skf.NSConn, *RoomUser]),
		//broadcastMsg: make(chan rchanr.ChanRepWithArguments[*skf.Message, []*RoomUser]),
		//players:     newSeatManager(),


		engine:       e,
		Users:        make(map[*RoomUser]struct{}),
		name:         tableName,
		Id:           tableId,
	}
	//新的一副牌
	NewDeck(cbGame)
	//開放入座
	go cbGame.OpenSeat()

	return cbGame
}

type Game struct { // 玩家進入房間, 玩家進入遊戲,玩家離開房間,玩家離開遊戲
	// 未來 當遊戲桌關閉時,記得一同關閉channel 以免leaking
	roomManager *RoomManager
	//gateway      rchanr.ChanReqWithArguments[*RoomUser, *chanResult]
	//findUser     rchanr.ChanReqWithArguments[*skf.NSConn, *RoomUser]
	//broadcastMsg rchanr.ChanReqWithArguments[*skf.Message, []*RoomUser]
	//players *SeatManager // // 遊戲座位環形,環形元素是RingItem(宣告在底下), 一場遊戲限四個人

	engine  *Engine

	roundSuitKeeper *RoundSuitKeep

	// Key: Ring裡的座位指標(SeatItem.Name), Value:牌指標
	// 並且同步每次出牌結果(依照是哪一家打出什牌並該手所打出的牌設成0指標
	Deck map[*uint8][]*uint8
	//遊戲中各家的持牌,會同步手上的出牌,打出的牌會設成0x0 CardCover
	deckInPlay map[uint8]*[NumOfCardsOnePlayer]uint8

	//代表遊戲中一副牌,從常數集合複製過來,參:dealer.NewDeck
	deck [NumOfCardsInDeck]*uint8

	//在_OnRoomJoined階段,透過 Game.userJoin 加入Users
	Users map[*RoomUser]struct{} // 進入房間者們 Key:玩家座標  value:玩家入桌順序.  一桌只限50人

	name string //table name
	Id   int32  // table Id (EntityId)

	ticketSN int //目前房間人數流水號,從1開始

	// 遊戲進行中出牌數計數器,當滿52張出牌表示遊戲局結算,遊戲結束
	countingInPlayCard uint8
}

func (g *Game) setSeatAndGetNextPlayer(seat uint8) uint8 { return 0 }

func (g *Game) setEnginePlaySeat(current uint8, next uint8) {}

// UserJoin 進入遊戲房,回傳nil表示正常進入
func (g *Game) UserJoin(ns *skf.NSConn, userName string, userZone uint8) *chanResult {
	return  g.roomManager.UserJoin(ns, userName, userZone)
}
// UserLeave 離開遊戲房
func (g *Game) UserLeave( ns *skf.NSConn, userName string, userZone uint8) *chanResult {
	return g.roomManager.UserLeave(ns, userName, userZone )
}

// PlayerJoinChannel 玩家入座
func (g *Game) PlayerJoinChannel(*skf.NSConn) (status SeatStatusAndGameStart, seatAt uint8, nextBidder uint8, forbidden []uint8, err error) {
	return SeatGameNA, 0, 0, nil, nil
}

// PlayerLeaveChannel 玩家離座
func (g *Game) PlayerLeaveChannel(*skf.NSConn) *chanResult { return nil }

// BidMux 傳入msg.Body 表示純叫品 (byte), seat8可從NsConn.Store獲取
func (g *Game) BidMux(seat8, bid8 uint8) {}

//BidMux(seat8, bid8 uint8) error

// PlayMux
// role 表示那一個連線角色(CbRole)執行PlayMux這個動作,可由Store獲取
// play8 玩家出的牌(可能會是莊家打夢家的牌,參考role)
func (g *Game) PlayMux(role CbRole, seat8, play8 uint8) {}

// SendDeal 執行發牌, player
func (g *Game) PlayerBid(player uint8, forbidden []uint8) {}

func (g *Game) broadcast(msg *skf.Message) (brokes []*RoomUser) { return nil }

// Broadcast 對遊戲房間進行遊戲相關資料廣播, Message.Body若送proto必須設定 protoBody 為true,其它資料型態都設成false
func (g *Game) Broadcast(sender fmt.Stringer, serialized []byte, namespace, event, room string, protoBody bool, err error) {
}

// BroadcastProto 對遊戲房間進行遊戲相關資料廣播(payload 指定要用 protocol buff)
func (g *Game) BroadcastProto(sender fmt.Stringer, event string, data pb.Message) {}

// BroadcastByte 對遊戲房間進行遊戲相關資料廣播 (payload 指定要用 byte (Poker,Bid, Seat>>6)
func (g *Game) BroadcastByte(sender fmt.Stringer, event string, data uint8) {}

// BroadcastBytes 對遊戲房間進行遊戲相關資料廣播 (payload 指定要用 bytes)
func (g *Game) BroadcastBytes(sender fmt.Stringer, event string, data []uint8) {}

// BroadcastString 對遊戲房間進行遊戲相關資料廣播 (payload 指定要用 String)
func (g *Game) BroadcastString(sender fmt.Stringer, event string, data []uint8) {}

func (g *Game) Emit(seat uint8, event string, content []uint8) {}

/*** 底下 SendXXX 有別於 Broadcast, Send打頭有分別送出,個別送出之意 ***/

func (g *Game) SendDeal()                                                       {}
func (g *Game) sendDataToClients(players []payloadData, eventClient string)     {}
func (g *Game) sendDataToClinet(seat uint8, eventClient string, payload []byte) {}
func (g *Game) sendProtoToClients(players []payloadData, eventClient string)    {}
func (g *Game) sendProto(seat uint8, eventClient string, payload pb.Message)    {}

func (g *Game) allowCards(seat uint8) []uint8                             { return nil }
func (g *Game) syncPlayCard(seat uint8, playCard uint8) (sync bool)       { return false }
func (g *Game) isCardOwnByPlayer(seat uint8, playCard uint8) (valid bool) { return false }
func (g *Game) userJoin(user *RoomUser) (res *chanResult)                 { return nil }
func (g *Game) userLeave(user *RoomUser) *chanResult                      { return nil }
func (g *Game) playerJoin(player *RoomUser) (res *chanResult)             { return nil }
func (g *Game) playerLeave(player *RoomUser) (res *chanResult)            { return nil }
func (g *Game) nsConnBySeat(seat uint8) *skf.NSConn                       { return nil }
func (g *Game) checkHand(seat uint8, cards [13]uint8)                     {}
func (g *Game) getRoomUser(ns *skf.NSConn) *RoomUser                      { return nil }

// OpenSeat 開放入座
func (g *Game) OpenSeat() {
	for {
		select {
		//坑: 這裡只能針對 gateway channel
		case tracking := <-g.gateway:
			user := tracking.Question
			switch user.Tracking {
			case EnterRoom:
				// ref UserJoinChannel
				tracking.Response <- g.userJoin(user)
			case LeaveRoom:
				tracking.Response <- g.userLeave(user)
			case EnterGame:
				tracking.Response <- g.playerJoin(user)
			case LeaveGame:
				tracking.Response <- g.playerLeave(user)
			}
		case ask := <-g.findUser:
			nsConn := ask.Question
			ask.Response <- g.getRoomUser(nsConn)

		case send := <-g.broadcastMsg:
			msg := send.Question
			send.Response <- g.broadcast(msg)
		case <-g.checkUsers:
		//	g.rmClosedUsers()
		default:
			// 移除突然斷線的user
			//g.rmClosedUsers()

		}
	}
}

// Start (洗牌) 每一次新局遊戲開始,洗牌,並設定玩家出牌,叫牌優先順序
func (g *Game) start() (bidder uint8, forbidden []uint8, done bool) {

	//在此洗牌
	Shuffle(g)

	//todo engine 決定誰先開始叫牌
	bidder, forbidden, done = g.engine.GetNextBid(0x00, openBidding, 0x00)
	//player := g.setSeatAndGetNextPlayer(bidder)
	nextPlayer := g.setSeatAndGetNextPlayer(bidder)
	g.setEnginePlaySeat(bidder, nextPlayer)
	return
}
