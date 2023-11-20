package game

//go:generate stringer -type=CbSeat,CbBid,CbCard,CbSuit,Track,CbRole,SeatStatusAndGameStart --linecomment -output cb32.enum_strings.go

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/moszorn/pb"
	"github.com/moszorn/utils/skf"
)

// '\u2660', /*♠*/
// '\u2661', /*♡*/
// '\u2662', /*♢*/
// '\u2663', /*♣*/
// '\u2664', /*♤*/
// '\u2665', /*♥*/
// '\u2666', /*♦*/
// '\u2667', /*♧*/

/*
System const
*/
const (

	//RoomUsersLimit 一個房間容納人數限制
	RoomUsersLimit = 100

	// PlayersLimit 一場遊戲人數限制
	PlayersLimit int = 4

	// KeyRoom 用於記錄(檢驗)使用者是否不正常斷線,設定KeyZone表示一定是設定了KeyRoom 表示玩家是否已經進入房間 (UserJoin設定),(UserLeave取消)
	KeyRoom string = "USER_IN_ROOM"
	// KeyZone 表連線登入房間哪個Zone,設定了KeyRoom也一併會設定KeyZone (UserJoin), 當不正常斷線時,可以從Store取出,進行RoomManager- UserLeave與PlayerLeave (UserJoin設定)(UserLeave取消)
	KeyZone string = "ZONE"
	// KeyGame 用於記錄(檢驗)使用者是否不正常斷線, KeyGame若存在應該會與KeyZone同值 表示玩家是否在遊戲中 (PlayerJoin設定),(PlayerLeave取消)
	KeyGame string = "GAME_SEAT"
	// KeyPlayRole 儲存/移除遊戲中各家的角色用於 Connection Store
	KeyPlayRole string = "ROLE"
)

type SeatStatusAndGameStart uint8

const (
	// SeatGameNA 保留
	SeatGameNA SeatStatusAndGameStart = iota
	// SeatFullBecauseGameStart 搶不到位置,因為遊戲已經開始
	SeatFullBecauseGameStart
	// SeatGetButGameWaiting 搶到位置,但遊戲座尚未滿座,遊戲尚未開始
	SeatGetButGameWaiting
	// SeatGetAndStartGame 搶到位置,且這次入座使遊戲剛好滿座,遊戲可以立刻開始
	SeatGetAndStartGame
)

// 底下CbXxx 透過stringer進行字串顯示Debug用
type (
	CbCard uint8
	CbBid  uint8
	CbSeat uint8
	CbSuit uint8
	CbRole uint8
)

const (
	RoleNotYet CbRole = iota //競叫尚未底定
	Audience                 //👨‍👨‍👧‍👧
	Defender                 // 🙅🏻‍♂️
	Declarer                 // 🥷🏻
	Dummy                    // 🙇🏼
)

// _e:east _s:south _w:west _n:north, enum CbSeat
const (
	//1個byte = 8個bit,扣除表符號得最高位元,2的7次方
	east  uint8 = 0x0      //0x00
	south uint8 = 0x1 << 6 //0x40
	west  uint8 = 0x2 << 6 //0x80
	north uint8 = 0x3 << 6 //0xC0

	CbEast      = CbSeat(east)        //東
	CbSouth     = CbSeat(south)       //南
	CbWest      = CbSeat(west)        //西
	CbNorth     = CbSeat(north)       //北
	CbSeatEmpty = CbSeat(valueNotSet) //空位
)

// 儲存叫牌過程中最後由哪一方叫到王
// 參考 biduint8.go - rawBidSuitMapper
// 參考 gamengines.go - cacheBidHistories
const (
	CLUB     CbSuit = iota //♣️
	DIAMOND                //♦️
	HEART                  //♥️
	SPADE                  //♠️
	TRUMP                  //👑
	DOUBLE                 //👩‍👦
	REDOUBLE               //👩‍👩‍👧‍👦
	PASS                   //👀PASS
)

/*
dartlang 中 enum表示
//Poker
/*
    var club4 = Pok.C4;

	enum Pok {
		C1(x:0,y:12,v:0x1),
		ST(x:20,y:28,v:0x27),
		SJ(x:30,y:47,v:0x28),
		SQ(x:43,y:22,v:0x29),
		SK(x:53,y:18,v:0x2a),
		SA(x:81,y:3,v:0x2b);

	final int x; sprite圖片X座標
	final int y; sprite圖片Y座標
	final int v; 牌值

	const Pok({required this.x,required this.y,required this.v });

	static Pok parse(int value) {
		switch(value) {
		 case 0x1:
		   return C1;
		}
		throw Exception('Unknown pok value');
	  }
	}
*/

// enum CbCard牌
const (
	BaseCover  CbCard = iota //🀫
	Club2                    // ♣️2
	Club3                    // ♣️3
	Club4                    //♣️4
	Club5                    // ♣️5
	Club6                    // ♣️6
	Club7                    // ♣️7
	Club8                    // ♣️8
	Club9                    // ♣️9
	Club10                   // ♣️10
	ClubJ                    //♣️J
	ClubQ                    //♣️Q
	ClubK                    //♣️K
	ClubAce                  //♣️A
	Diamond2                 //♦️2
	Diamond3                 //♦️3
	Diamond4                 //♦️4
	Diamond5                 //♦️5
	Diamond6                 //♦️6
	Diamond7                 //♦️7
	Diamond8                 //♦️8
	Diamond9                 //♦️9
	Diamond10                //♦️10
	DiamondJ                 //♦️J
	DiamondQ                 //♦️Q
	DiamondK                 //♦️K
	DiamondAce               //♦️A
	Heart2                   //♥️2
	Heart3                   //♥️3
	Heart4                   //♥️4
	Heart5                   //♥️5
	Heart6                   //♥️6
	Heart7                   //♥️7
	Heart8                   //♥️8
	Heart9                   //♥️9
	Heart10                  //♥️10
	HeartJ                   //♥️J
	HeartQ                   //♥️Q
	HeartK                   //♥️K
	HeartAce                 //♥️A
	Spade2                   //♠️2
	Spade3                   //♠️3
	Spade4                   //♠️4
	Spade5                   //♠️5
	Spade6                   //♠️6
	Spade7                   //♠️7
	Spade8                   //♠️8
	Spade9                   //♠️9
	Spade10                  //♠️10
	SpadeJ                   //♠️J
	SpadeQ                   //♠️Q
	SpadeK                   //♠️K
	SpadeAce                 //♠️A
)

// enum CbBid  ♣️♦️♥️♠️ ♛  ✘   ✗✘✓✔︎
const (
	Pass1 CbBid = iota + 1 //1線✔︎
	C1                     //1線♣️
	D1                     //1線♦️
	H1                     //1線♥️
	S1                     //1線♠️
	NT1                    //1線♛
	Db1                    //1線✘
	Db1x2                  //1線✗✘
	Pass2                  //2線✔︎
	C2                     //2線♣️
	D2                     //2線♦️
	H2                     //2線♥️
	S2                     //2線♠️
	NT2                    //2線♛
	Db2                    //2線✘
	Db2x2                  //2線✗✘
	Pass3                  //3線✔︎
	C3                     //3線♣️
	D3                     //3線♦️
	H3                     //3線♥️
	S3                     //3線♠️
	NT3                    //3線♛
	Db3                    //3線✘
	Db3x2                  //3線✗✘
	Pass4                  //4線✔︎
	C4                     //4線♣️
	D4                     //4線♦️
	H4                     //4線♥️
	S4                     //4線♠️
	NT4                    //4線♛
	Db4                    //4線✘
	Db4x2                  //4線✗✘
	Pass5                  //5線✔︎
	C5                     //5線♣️
	D5                     //5線♦️
	H5                     //5線♥️
	S5                     //5線♠️
	NT5                    //5線♛
	Db5                    //5線✘
	Db5x2                  //5線✗✘
	Pass6                  //6線✔︎
	C6                     //6線♣️
	D6                     //6線♦️
	H6                     //6線♥️
	S6                     //6線♠️
	NT6                    //6線♛
	Db6                    //6線✘
	Db6x2                  //6線✗✘
	Pass7                  //7線✔︎
	C7                     //7線♣️
	D7                     //7線♦️
	H7                     //7線♥️
	S7                     //7線♠️
	NT7                    //7線♛
	Db7                    //7線✘
	Db7x2                  //7線✗✘
)

var (
	CbCardUint8s = [52]uint8{uint8(Club2), uint8(Club3), uint8(Club4), uint8(Club5), uint8(Club6), uint8(Club7), uint8(Club8), uint8(Club9), uint8(Club10), uint8(ClubJ), uint8(ClubQ), uint8(ClubK), uint8(ClubAce), uint8(Diamond2), uint8(Diamond3), uint8(Diamond4), uint8(Diamond5), uint8(Diamond6), uint8(Diamond7), uint8(Diamond8), uint8(Diamond9), uint8(Diamond10), uint8(DiamondJ), uint8(DiamondQ), uint8(DiamondK), uint8(DiamondAce), uint8(Heart2), uint8(Heart3), uint8(Heart4), uint8(Heart5), uint8(Heart6), uint8(Heart7), uint8(Heart8), uint8(Heart9), uint8(Heart10), uint8(HeartJ), uint8(HeartQ), uint8(HeartK), uint8(HeartAce), uint8(Spade2), uint8(Spade3), uint8(Spade4), uint8(Spade5), uint8(Spade6), uint8(Spade7), uint8(Spade8), uint8(Spade8), uint8(Spade10), uint8(SpadeJ), uint8(SpadeQ), uint8(SpadeK), uint8(SpadeAce)}
	CbBidUint8s  = [56]uint8{uint8(Pass1), uint8(C1), uint8(D1), uint8(H1), uint8(S1), uint8(NT1), uint8(Db1), uint8(Db1x2), uint8(Pass2), uint8(C2), uint8(D2), uint8(H2), uint8(S2), uint8(NT2), uint8(Db2), uint8(Db2x2), uint8(Pass3), uint8(C3), uint8(D3), uint8(H3), uint8(S3), uint8(NT3), uint8(Db3), uint8(Db3x2), uint8(Pass4), uint8(C4), uint8(D4), uint8(H4), uint8(S4), uint8(NT4), uint8(Db4), uint8(Db4x2), uint8(Pass5), uint8(C5), uint8(D5), uint8(H5), uint8(S5), uint8(NT5), uint8(Db5), uint8(Db5x2), uint8(Pass6), uint8(C6), uint8(D6), uint8(H6), uint8(S6), uint8(NT6), uint8(Db6), uint8(Db6x2), uint8(Pass7), uint8(C7), uint8(D7), uint8(H7), uint8(S7), uint8(NT7), uint8(Db7), uint8(Db7x2)}
	CbSeatUint8s = [4]uint8{east, south, west, north}
)

// Track 使用者軌跡(Lobby,Room)(protobuf)
type Track int8

const (
	IddleTrack Track = iota // 無法追蹤,enum暫時沒用
	EnterRoom               //進入房間(或離開遊戲)
	LeaveRoom               //離開房間 (前端觸動)
	EnterGame               //進入遊戲(或從房間進入)
	LeaveGame               //離開遊戲 (前端觸動)

)

type tableTopic int8

const (
	IsPlayerOnSeat   tableTopic = iota //查詢user已經存在遊戲桌中
	IsGameStart                        // 查詢遊戲人數是否已滿四人(開始)
	SeatShift                          //移動座位
	PlayerAction                       //表示使用者出牌,需要與RoomManager Ring同步
	_GetTablePlayers                   //請求撈出桌面正在遊戲的玩家 (底線打頭表示只限roomManager內部使用
	_GetZoneUsers                      //請求撈出Zone中的觀眾使用者,也包含四家玩者
	_FindPlayer                        //請求找尋指定玩家連線
	_GetTableInfo                      //請求取得房間觀眾,空位起點依序的玩家座位
)

/*
 pb 與 DDD entity 整合
*/

type (
	RoomUser struct {
		NsConn *skf.NSConn

		Tracking Track

		//TicketTime time.Time //  入房間時間,若在Ring中表示上桌的時間
		//Bid  uint8 //所叫的叫品
		//Play uint8 //所出的牌
		//Name   string
		//Zone   uint8 /*east south west north*/

		*pb.PlayingUser       // 坑:要注意,PlayingUser不是用 Reference
		Zone8           uint8 // 從 PlayingUser Zone轉型過來,放在Zone8是為了方便取用
		IsClientBroken  bool  //是否不正常離線(在KickOutBrokenConnection 設定)
	}

	Audiences []*RoomUser //代表非玩家的旁賽者
)

func (ru *RoomUser) Ticket() {
	ru.TicketTime = pb.LocalTimestamp(time.Now())
}
func (ru *RoomUser) TicketString() string {
	return ru.TicketTime.AsTime().Format("01/02 15:04:05")
}

// Connections 所有觀眾連線
func (audiences Audiences) Connections() (connections []*skf.NSConn) {
	for i := range audiences {
		if audiences[i].NsConn.Conn.IsClosed() {
			continue
		}
		connections = append(connections, audiences[i].NsConn)
	}
	return
}

// DumpNames 列出觀眾姓名, debug用
func (audiences Audiences) DumpNames(dbgString string) {
	slog.Debug(dbgString)
	for i := range audiences {
		if audiences[i].NsConn.Conn.IsClosed() {
			slog.Debug("觀眾(Audience)", slog.String(audiences[i].Name, "斷線"))
			continue
		}
		slog.Debug("觀眾(Audience)", slog.String(audiences[i].Name, fmt.Sprintf("%s", CbSeat(audiences[i].Zone8))))
	}
}

/************************************************************************************/

const (
	//ValueMark8 求值(CbBid, CbCard)用 example: CbBid(valueMark8 & raw8) CbCard(valueMark8 & raw8)
	valueMark8 uint8 = 0x3F
	//SeatMark8 求座位(CbSeat), example: CbSeat(seatMark8 & raw8)
	seatMark8 uint8 = 0xC0

	//首引訊號
	openLeading uint8 = 0x0
	//新局開叫
	openBidding uint8 = 0x0

	//valueNotSet 表示值未定,因為x00被用於其他意義上
	valueNotSet uint8 = 0x88
)

// Poker by byte & Deck of Poker
const (
	_cover     uint8 = 0x0
	club2      uint8 = 0x1
	club3      uint8 = 0x2
	club4      uint8 = 0x3
	club5      uint8 = 0x4
	club6      uint8 = 0x5
	club7      uint8 = 0x6
	club8      uint8 = 0x7
	club9      uint8 = 0x8
	club10     uint8 = 0x9
	clubJ      uint8 = 0xA
	clubQ      uint8 = 0xB
	clubK      uint8 = 0xC
	clubAce    uint8 = 0xD
	diamond2   uint8 = 0xE
	diamond3   uint8 = 0xF
	diamond4   uint8 = 0x10
	diamond5   uint8 = 0x11
	diamond6   uint8 = 0x12
	diamond7   uint8 = 0x13
	diamond8   uint8 = 0x14
	diamond9   uint8 = 0x15
	diamond10  uint8 = 0x16
	diamondJ   uint8 = 0x17
	diamondQ   uint8 = 0x18
	diamondK   uint8 = 0x19
	diamondAce uint8 = 0x1A
	heart2     uint8 = 0x1B
	heart3     uint8 = 0x1C
	heart4     uint8 = 0x1D
	heart5     uint8 = 0x1E
	heart6     uint8 = 0x1F
	heart7     uint8 = 0x20
	heart8     uint8 = 0x21
	heart9     uint8 = 0x22
	heart10    uint8 = 0x23
	heartJ     uint8 = 0x24
	heartQ     uint8 = 0x25
	heartK     uint8 = 0x26
	heartAce   uint8 = 0x27
	spade2     uint8 = 0x28
	spade3     uint8 = 0x29
	spade4     uint8 = 0x2A
	spade5     uint8 = 0x2B
	spade6     uint8 = 0x2C
	spade7     uint8 = 0x2D
	spade8     uint8 = 0x2E
	spade9     uint8 = 0x2F
	spade10    uint8 = 0x30
	spadeJ     uint8 = 0x31
	spadeQ     uint8 = 0x32
	spadeK     uint8 = 0x33
	spadeAce   uint8 = 0x34
)

var (
	//常數
	playerSeats = [4]uint8{east, south, west, north}
	//playerSeats = [4]uint8{east, north, west, south}

	//常數
	deck = [NumOfCardsInDeck]uint8{club2, club3, club4, club5, club6, club7, club8, club9, club10, clubJ, clubQ, clubK, clubAce, diamond2, diamond3, diamond4, diamond5, diamond6, diamond7, diamond8, diamond9, diamond10, diamondJ, diamondQ, diamondK, diamondAce, heart2, heart3, heart4, heart5, heart6, heart7, heart8, heart9, heart10, heartJ, heartQ, heartK, heartAce, spade2, spade3, spade4, spade5, spade6, spade7, spade8, spade9, spade10, spadeJ, spadeQ, spadeK, spadeAce}
)

// CardRange 區間, 限制該回合能打出牌的範圍
type CardRange [2]uint8

var (
	// 王張區間
	CKings CardRange = [2]uint8{club2, clubAce}
	DKings CardRange = [2]uint8{diamond2, diamondAce}
	HKings CardRange = [2]uint8{heart2, heartAce}
	SKings CardRange = [2]uint8{spade2, spadeAce}
	NKings CardRange = [2]uint8{club2, spadeAce}

	// CardRange 回合允許出牌的區間,若出牌不在區間只有墊牌與切牌兩種可能
	ClubRange    = *(&CKings)
	DiamondRange = *(&DKings)
	HeartRange   = *(&HKings)
	SpadeRange   = *(&SKings)
	TrumpRange   = *(&NKings)
)

// 從uint32取出一個uint8值(取出uint32轉[]byte的索引0(第一個byte))
func uint32ToUint8(value uint32) uint8 {
	values := make([]byte, 4)
	binary.LittleEndian.PutUint32(values, value)
	return values[0]
}

// uint32ToValue 從封包以LittleEndian將uint32轉換回有效的資料(uint8)
// 回傳seat(CbSeat),value(CbCard,CbBid,CbSuit), orig (raw8)(從封包LittleEndian取出的原uin8資料),
func uint32ToValue(value32 uint32) (seat, value, orig uint8) {
	value8 := uint32ToUint8(value32)
	return value8 & seatMark8, value8 & valueMark8, value8
}

// cards13x4ToBytes 將四家的牌兜成一個 []byte
func cards13x4ToBytes(c1, c2, c3, c4 [13]*uint8) (protoAttr []byte) {
	values := make([]uint8, 0, 52)
	players := [4][13]*uint8{c1, c2, c3, c4}
	for p := range players {
		cards := players[p]
		for i := 0; i < len(cards); i++ {
			values = append(values, *cards[i])
		}
	}
	buf := bytes.NewBuffer(nil)
	err := binary.Write(buf, binary.LittleEndian, values)
	if err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// 注意 坑坑: b 不能大於 128 (0x80) 不然當data overflow
func seatToBytes(b uint8) (protoAttr []byte) {
	values := make([]uint8, 0, 1)

	values = append(values, bitRShift(b))
	buf := bytes.NewBuffer(nil)

	err := binary.Write(buf, binary.LittleEndian, values)

	if err != nil {
		panic(err)
	}

	return buf.Bytes()
}

// uint8Seat 因為socket傳輸byte最大到0X7F,所以 Seat (西0x80,北0xC0)都不能直接傳送,必須轉成0x0,0x1,0x2,0x3)
// 注意 坑坑: b 不能大於 128 (0x80) 不然當data overflow
func bitRShift(b uint8) uint8 {
	var rightShift uint8
	switch b {
	case east:
		//0x0 (0000)
		return b
	case south:
		//0x40轉回0x1 (0001)
		fallthrough
	case west:
		//0x80轉回0x2 (0010)
		fallthrough
	case north:
		//0xC0轉回0x3 (0011)
		rightShift = b >> 6
	}
	return rightShift
}

// 注意 坑: 不可能將seat轉成單一的byte放到 []byte, 因為西(0x80->1000 0000已經overflow)
// seatToBytes 將Seat直接轉成bytes
/*func seatToBytes(seat8 uint8) (seat []byte) {
	seat = make([]uint8, 4, 4)

	binary.LittleEndian.PutUint32(seat, uint32(bitRShift(seat8)))
	return
}*/

// bytesToSeat 將bytes轉回seat
func bytesToSeat(seat []byte) uint8 {
	return uint8(binary.LittleEndian.Uint32(seat))
}

// 將一家13張牌轉成bytes ,可以用protocol buffer 屬性
func cardsToBytes(cardsPointers [13]*uint8) (cards []byte) {
	cards = make([]uint8, 0, 13)
	for i := 0; i < len(cardsPointers); i++ {
		cards = append(cards, *cardsPointers[i])
	}
	return cards
}

func TrumpCardRange(trump uint8) CardRange {
	//trump是遊戲最後的叫品

	//從最後叫品找出該局的王是什花色(王張區間) Suit
	switch CbSuit(rawBidSuitMapper[trump]) {
	case CLUB:
		return CKings
	case DIAMOND:
		return DKings
	case HEART:
		return HKings
	case SPADE:
		return SKings
	case TRUMP:
		return NKings
		// 無王沒有王張區間,所以表示ㄉ52張都可以出
	case PASS:
		// 無王沒有王張區間
	case DOUBLE:
	case REDOUBLE:
	}
	return [2]uint8{0x0, 0x0}
}

// PlayCardRange 回合允許出牌的區間,該回合首打決定所要出牌的花色與區間
func PlayCardRange(firstHand uint8) CardRange {

	//模擬四家的出牌
	// first首打
	var first = CbCard(firstHand)
	fmt.Printf("%08b %[1]d ", first)

	switch {
	case first < Diamond2:
		fmt.Printf("Club[%08b ~ %08b]\n", Club2, ClubAce)
		return ClubRange
	case ClubAce < first && first < Heart2:
		fmt.Printf("Diamond[%08b ~ %08b]\n", Diamond2, DiamondAce)
		return DiamondRange
	case DiamondAce < first && first < Spade2:
		fmt.Printf("Heart[%08b ~ %08b]\n", Heart2, HeartAce)
		return HeartRange
	case HeartAce < first && first <= SpadeAce:
		fmt.Printf("Spade[%08b ~ %08b]\n", Spade2, SpadeAce)
		return SpadeRange
	default:
	}
	return [2]uint8{0x0, 0x0}
}

// RoundSuitKeep 紀錄該回合能出的牌範圍, 本局贏家將ReNewKeeper,直到client送來該玩家打出的牌 DoKeep,所有玩家可出的牌被限定於RoundSuitKeep
type RoundSuitKeep struct {
	Player    uint8     //keep 持續等待該玩家下一次出牌
	CardRange CardRange //當該玩家(Player)出牌時,依照所出的牌(Suit)找出可出牌最大最小範圍
	Min       uint8     // 最小可出牌
	Max       uint8     //最大可出牌
	IsSet     bool      //是否已經設定要keep的seat
}

// NewRoundSuitKeep 只能以首引生成
func NewRoundSuitKeep(firstLead uint8) *RoundSuitKeep {
	return &RoundSuitKeep{
		Player:    firstLead,
		CardRange: CardRange{},
		IsSet:     true,
		Min:       0,
		Max:       0,
	}
}

/* 使用 RoundSuitKeep 順序依須說明
1. NewRoundSuitKeep 首引時建立 Keeper (參考:game.go:BidMux.NewRoundSuitKeep)
2. 首引打出牌後, DoKeep只會紀錄首引打出的Suit
3. 其他玩家持續DoKeep都不會被紀錄 (參考: game.go:PlayMux)
4. 以當前Keep紀錄的suit來設定下一個玩家能打出的牌(因為前端會需要用來判斷是否可以double click out)
5. 回合結束比較輸贏後,以本回合贏者重新設定下一輪的RoundSuitKeep
6. 上一輪贏者打出第一張牌後, DoKeep只會紀錄他打出的Suit
7. 其他玩家持續DoKeep都不會被紀錄
8. 持續以當前Keep紀錄的suit來設定下一個玩家能打出的牌(因為前端會需要用來判斷是否可以double click out)
9. 遊戲結束比較輸贏後,將RoundSuitKeep設定為nil,直到下一輪叫品王牌出來,首引決定時 NewRoundSuitKeep會再一次被執行
*/

// DoKeep 傳入打牌者,打什麼牌,若出牌者是keeper則算出range並紀錄
func (r *RoundSuitKeep) DoKeep(seat, card uint8) error {
	if !r.IsSet {
		return errors.New("RoundSuitKeep尚未設定")
	}
	if seat != r.Player {
		//不是首打出牌玩家,回傳return不記
		return nil
	}

	r.CardRange = PlayCardRange(card)
	r.Min = r.CardRange[0]
	r.Max = r.CardRange[1]
	return nil
}

// ReNewKeeper 更換玩家, valueNotSet是 zeroValue
func (r *RoundSuitKeep) ReNewKeeper(seat uint8) {
	r.Player = seat
	r.IsSet = true
	r.Min = uint8(club2)
	r.Max = uint8(spadeAce)
}

// AllowCardsByRoundSuitKeep cards玩家當前手上持牌(cards),allows 玩家下次可出的牌(allows)
// 玩家可打出的牌,是依據回合先出牌的suit決定
// 情境:
//  0. 必須以首打出牌的Suit為依據,首打打出麼suit,就要跟打什麼suit.
//  1. 若手上持牌無可出的suit,可打王牌,可打任何張墊牌
//     必須知道:
func (r *RoundSuitKeep) AllowCardsByRoundSuitKeep(cards *[13]uint8) []uint8 {
	//找出未標示 game.BaseCover表示玩家尚未出過牌

	//followSuit以首打為依據
	followSuits := make([]uint8, 0, 13)
	//unfollowSuit不以首打為依據
	unfollowSuits := make([]uint8, 0, 13)

	for i := range cards {
		if cards[i] == uint8(BaseCover) {
			continue
		}
		if r.Min <= cards[i] && r.Max >= cards[i] {
			followSuits = append(followSuits, cards[i])
		}
		unfollowSuits = append(unfollowSuits, cards[i])
	}
	if len(followSuits) != 0 {
		return followSuits
	}
	return unfollowSuits
}

//gamengine.trumpRange
// PlayCardRange(firstHand uint8) CardRange
/*
	switch game.CbSuit(c.trumpSuit) {
	case game.TRUMP:
		winner = c.playResultInTrump(eastCard, southCard, westCard, northCard)
	default:
		winner = c.playResultInSuit(eastCard, southCard, westCard, northCard)
	}
*/

/*
//dartlang中的叫品
	var seat = 0x3 << 6;  //北
	var bid = 0xd;        //S2

	//叫品
	var bidding = seat | bid;
	print('$bidding ${bidding.toRadixString(16)} '); // 205 cd
*/

var (

	// seatsBidsTable 存放各家的1PASS,1C,1D,1H,1S,1NT,1X,1XX,....7PASS,7C,7D,7H,7S,7NT,7X,7XX
	// key: seat, value(7線叫品) => PASS,C,D,H,S,NT,X,XX
	/*
		seatsBidsTable = map[ uint8][56]uint8{
			0x0:  {0x1 , 0x2 , 0x3 , 0x4 , 0x5 , 0x6 , 0x7 , 0x8 , 0x9 , 0xA , 0xB, 0xC, 0xD, 0xE, 0xF, 0x10, 0x11 , 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38},
			0x40: {0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F, 0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5A, 0x5B, 0x5C, 0x5D, 0x5E, 0x5F, 0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6A, 0x6B, 0x6C, 0x6D, 0x6E, 0x6F, 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78},
			0x80: {0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8A, 0x8B, 0x8C, 0x8D, 0x8E, 0x8F, 0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9A, 0x9B, 0x9C, 0x9D, 0x9E, 0x9F, 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0xA7, 0xA8, 0xA9, 0xAA, 0xAB, 0xAC, 0xAD, 0xAE, 0xAF, 0xB0, 0xB1, 0xB2, 0xB3, 0xB4, 0xB5, 0xB6, 0xB7, 0xB8},
			0xC0: {0xC1, 0xC2, 0xC3, 0xC4, 0xC5, 0xC6, 0xC7, 0xC8, 0xC9, 0xCA, 0xCB, 0xCC, 0xCD, 0xCE, 0xCF, 0xD0, 0xD1, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7, 0xD8, 0xD9, 0xDA, 0xDB, 0xDC, 0xDD, 0xDE, 0xDF, 0xE0, 0xE1, 0xE2, 0xE3, 0xE4, 0xE5, 0xE6, 0xE7, 0xE8, 0xE9, 0xEA, 0xEB, 0xEC, 0xED, 0xEE, 0xEF, 0xF0, 0xF1, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7, 0xF8},
		}*/

	// Raw叫品(CbSeat+SbBid) 對應CbSuit, 座位加叫品找出是哪一個SUIT
	// 各家叫品rawBid8與Suit對應, Suit參考CbSuit
	// Key: rawBid8, value: Suit | rawBid8帶位置的叫品,參考seatsBidsTable
	// 當北家叫1C時,回傳 CLUB
	// 當東家叫3NT時,回傳 TRUMP
	// 當南家叫5S時,回傳 SPADE
	rawBidSuitMapper = map[uint8] /*rawBid8*/ uint8 /*Suit*/ {0xC2: 0x0, 0xCA: 0x0, 0xD2: 0x0, 0xDA: 0x0, 0xE2: 0x0, 0xEA: 0x0, 0xF2: 0x0, 0x2: 0x0, 0xA: 0x0, 0x12: 0x0, 0x1A: 0x0, 0x22: 0x0, 0x2A: 0x0, 0x32: 0x0, 0x42: 0x0, 0x4A: 0x0, 0x52: 0x0, 0x5A: 0x0, 0x62: 0x0, 0x6A: 0x0, 0x72: 0x0, 0x82: 0x0, 0x8A: 0x0, 0x92: 0x0, 0x9A: 0x0, 0xA2: 0x0, 0xAA: 0x0, 0xB2: 0x0, 0xC3: 0x1, 0xCB: 0x1, 0xD3: 0x1, 0xDB: 0x1, 0xE3: 0x1, 0xEB: 0x1, 0xF3: 0x1, 0x3: 0x1, 0xB: 0x1, 0x13: 0x1, 0x1B: 0x1, 0x23: 0x1, 0x2B: 0x1, 0x33: 0x1, 0x43: 0x1, 0x4B: 0x1, 0x53: 0x1, 0x5B: 0x1, 0x63: 0x1, 0x6B: 0x1, 0x73: 0x1, 0x83: 0x1, 0x8B: 0x1, 0x93: 0x1, 0x9B: 0x1, 0xA3: 0x1, 0xAB: 0x1, 0xB3: 0x1, 0xC4: 0x2, 0xCC: 0x2, 0xD4: 0x2, 0xDC: 0x2, 0xE4: 0x2, 0xEC: 0x2, 0xF4: 0x2, 0x4: 0x2, 0xC: 0x2, 0x14: 0x2, 0x1C: 0x2, 0x24: 0x2, 0x2C: 0x2, 0x34: 0x2, 0x44: 0x2, 0x4C: 0x2, 0x54: 0x2, 0x5C: 0x2, 0x64: 0x2, 0x6C: 0x2, 0x74: 0x2, 0x84: 0x2, 0x8C: 0x2, 0x94: 0x2, 0x9C: 0x2, 0xA4: 0x2, 0xAC: 0x2, 0xB4: 0x2, 0xC5: 0x3, 0xCD: 0x3, 0xD5: 0x3, 0xDD: 0x3, 0xE5: 0x3, 0xED: 0x3, 0xF5: 0x3, 0x5: 0x3, 0xD: 0x3, 0x15: 0x3, 0x1D: 0x3, 0x25: 0x3, 0x2D: 0x3, 0x35: 0x3, 0x45: 0x3, 0x4D: 0x3, 0x55: 0x3, 0x5D: 0x3, 0x65: 0x3, 0x6D: 0x3, 0x75: 0x3, 0x85: 0x3, 0x8D: 0x3, 0x95: 0x3, 0x9D: 0x3, 0xA5: 0x3, 0xAD: 0x3, 0xB5: 0x3, 0xC6: 0x4, 0xCE: 0x4, 0xD6: 0x4, 0xDE: 0x4, 0xE6: 0x4, 0xEE: 0x4, 0xF6: 0x4, 0x6: 0x4, 0xE: 0x4, 0x16: 0x4, 0x1E: 0x4, 0x26: 0x4, 0x2E: 0x4, 0x36: 0x4, 0x46: 0x4, 0x4E: 0x4, 0x56: 0x4, 0x5E: 0x4, 0x66: 0x4, 0x6E: 0x4, 0x76: 0x4, 0x86: 0x4, 0x8E: 0x4, 0x96: 0x4, 0x9E: 0x4, 0xA6: 0x4, 0xAE: 0x4, 0xB6: 0x4, 0xC7: 0x5, 0xCF: 0x5, 0xD7: 0x5, 0xDF: 0x5, 0xE7: 0x5, 0xEF: 0x5, 0xF7: 0x5, 0x7: 0x5, 0xF: 0x5, 0x17: 0x5, 0x1F: 0x5, 0x27: 0x5, 0x2F: 0x5, 0x37: 0x5, 0x47: 0x5, 0x4F: 0x5, 0x57: 0x5, 0x5F: 0x5, 0x67: 0x5, 0x6F: 0x5, 0x77: 0x5, 0x87: 0x5, 0x8F: 0x5, 0x97: 0x5, 0x9F: 0x5, 0xA7: 0x5, 0xAF: 0x5, 0xB7: 0x5, 0xC8: 0x6, 0xD0: 0x6, 0xD8: 0x6, 0xE0: 0x6, 0xE8: 0x6, 0xF0: 0x6, 0xF8: 0x6, 0x8: 0x6, 0x10: 0x6, 0x18: 0x6, 0x20: 0x6, 0x28: 0x6, 0x30: 0x6, 0x38: 0x6, 0x48: 0x6, 0x50: 0x6, 0x58: 0x6, 0x60: 0x6, 0x68: 0x6, 0x70: 0x6, 0x78: 0x6, 0x88: 0x6, 0x90: 0x6, 0x98: 0x6, 0xA0: 0x6, 0xA8: 0x6, 0xB0: 0x6, 0xB8: 0x6, 0xC1: 0x7, 0xC9: 0x7, 0xD1: 0x7, 0xD9: 0x7, 0xE1: 0x7, 0xE9: 0x7, 0xF1: 0x7, 0x1: 0x7, 0x9: 0x7, 0x11: 0x7, 0x19: 0x7, 0x21: 0x7, 0x29: 0x7, 0x31: 0x7, 0x41: 0x7, 0x49: 0x7, 0x51: 0x7, 0x59: 0x7, 0x61: 0x7, 0x69: 0x7, 0x71: 0x7, 0x81: 0x7, 0x89: 0x7, 0x91: 0x7, 0x99: 0x7, 0xA1: 0x7, 0xA9: 0x7, 0xB1: 0x7}
	// Key: rawBid8
	// 例如: 0xC2 -> 0xC0北 | 0x02 (CbBid表C1) => 北家叫1C
	//      0xCA -> 0xC0北 | 0x0A (CbBid表C2) => 北家叫2C
	// Value: CbSuit
	// 例如: 0x0 - CLUB
	//      0x1 - DIAMOND
	//      0x2 - HEART
	//      0x3 - SPADE
	//      0x4 - TRUMP
	//      0x5 - DOUBLE
	//      0x6 - REDOUBLE
	//      0x7 - PASS

	//1線到7線叫品, 不含seat
	pure7lineBid = [56]uint8{
		0x1,  /*PASS1*/
		0x2,  /*C1*/
		0x3,  /*D1*/
		0x4,  /*H1*/
		0x5,  /*S1*/
		0x6,  /*NT1*/
		0x7,  /*Db1*/
		0x8,  /*Db1x2*/
		0x9,  /*PASS2*/
		0xA,  /*C2*/
		0xB,  /*D2*/
		0xC,  /*H2*/
		0xD,  /*S2*/
		0xE,  /*NT2*/
		0xF,  /*Db2*/
		0x10, /*Db2x2*/
		0x11, /*Pass3*/
		0x12, /*C3*/
		0x13, /*D3*/
		0x14, /*H3*/
		0x15, /*S3*/
		0x16, /*NT3*/
		0x17, /*Db3*/
		0x18, /*Db3x2*/
		0x19, /*PASS4*/
		0x1A, /*C4*/
		0x1B, /*D4*/
		0x1C, /*H4*/
		0x1D, /*S4*/
		0x1E, /*NT4*/
		0x1F, /*Db4*/
		0x20, /*Db4x2*/
		0x21, /*PASS5*/
		0x22, /*C5*/
		0x23, /*D5*/
		0x24, /*H5*/
		0x25, /*S5*/
		0x26, /*NT5*/
		0x27, /*Db5*/
		0x28, /*Db5x2*/
		0x29, /*PASS6*/
		0x2A, /*C6*/
		0x2B, /*D6*/
		0x2C, /*H6*/
		0x2D, /*S6*/
		0x2E, /*NT6*/
		0x2F, /*Db6*/
		0x30, /*Db6x2*/
		0x31, /*PASS7*/
		0x32, /*C7*/
		0x33, /*D7*/
		0x34, /*H7*/
		0x35, /*S7*/
		0x36, /*NT7*/
		0x37, /*Db7*/
		0x38, /*Db7x2*/
	}
)

/* ********************************************************************************** */

// GameConstArg Game 常用常數包裝
type GameConstArg struct {
	ValueNotSet uint8
}

// GameConstantExport 將 Game 常用常數輸出給 project package
func GameConstantExport() *GameConstArg {
	return &GameConstArg{
		ValueNotSet: valueNotSet,
	}
}
