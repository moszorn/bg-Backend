package game

//go:generate stringer -type=CbSeat,CbBid,CbCard,CbSuit,Track,CbRole,SeatStatusAndGameStart --linecomment -output cb32.enum_strings.go

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/moszorn/pb"
	"github.com/moszorn/utils/skf"
)

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

const (
	// GamePlayCountDown 遊戲中,玩家叫/出牌時間, 未來(從DB撈取)依附在RoomUser中
	GamePlayCountDown uint32 = 15
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
	Audience                 //觀眾
	Defender                 //防家
	Declarer                 //莊家
	Dummy                    //夢家
)

// _e:east _s:south _w:west _n:north, enum CbSeat
const (
	//east(0x0) south(0x40) west(0x80) north(0xC0)

	east    CbSeat = iota << 6 //東
	south                      //南
	west                       //西
	north                      //北
	seatYet = 255              // 遊戲空位
)

// 儲存叫牌過程中最後由哪一方叫到王
const (
	CLUB     CbSuit = iota //♣️
	DIAMOND                //♦️
	HEART                  //♥️
	SPADE                  //♠️
	TRUMP                  //👑
	DOUBLE                 // double
	REDOUBLE               // double x2
	PASS                   //👀PASS
	ZeroSuit               //
)

// enum CbCard牌
const (
	BaseCover  CbCard = iota //🀫
	Club2                    // ♣️ 2
	Club3                    // ♣️ 3
	Club4                    //♣️ 4
	Club5                    // ♣️ 5
	Club6                    // ♣️ 6
	Club7                    // ♣️ 7
	Club8                    // ♣️ 8
	Club9                    // ♣️ 9
	Club10                   // ♣️ 10
	ClubJ                    //♣️ J
	ClubQ                    //♣️ Q
	ClubK                    //♣️ K
	ClubAce                  //♣️ A
	Diamond2                 //♦️ 2
	Diamond3                 //♦️ 3
	Diamond4                 //♦️ 4
	Diamond5                 //♦️ 5
	Diamond6                 //♦️ 6
	Diamond7                 //♦️ 7
	Diamond8                 //♦️ 8
	Diamond9                 //♦️ 9
	Diamond10                //♦️ 10
	DiamondJ                 //♦️ J
	DiamondQ                 //♦️ Q
	DiamondK                 //♦️ K
	DiamondAce               //♦️ A
	Heart2                   //♥️ 2
	Heart3                   //♥️ 3
	Heart4                   //♥️ 4
	Heart5                   //♥️ 5
	Heart6                   //♥️ 6
	Heart7                   //♥️ 7
	Heart8                   //♥️ 8
	Heart9                   //♥️ 9
	Heart10                  //♥️ 10
	HeartJ                   //♥️ J
	HeartQ                   //♥️ Q
	HeartK                   //♥️ K
	HeartAce                 //♥️ A
	Spade2                   //♠️ 2
	Spade3                   //♠️ 3
	Spade4                   //♠️ 4
	Spade5                   //♠️ 5
	Spade6                   //♠️ 6
	Spade7                   //♠️ 7
	Spade8                   //♠️ 8
	Spade9                   //♠️ 9
	Spade10                  //♠️ 10
	SpadeJ                   //♠️ J
	SpadeQ                   //♠️ Q
	SpadeK                   //♠️ K
	SpadeAce                 //♠️ A
)

// zeroBid 初始叫品表示開叫時叫品的值
//新局開叫
//zeroBid uint8 = 0x0

// enum CbBid  ♣️♦️♥️♠️ ♛  ✘   ✗✘✓✔︎
const (
	BidYet CbBid = iota // CbBid未設定,初始叫品表示開叫時叫品的值
	Pass1               //1線✔︎
	C1                  //1線♣️
	D1                  //1線♦️
	H1                  //1線♥️
	S1                  //1線♠️
	NT1                 //1線♛
	Db1                 //1線✘
	Db1x2               //1線✗✘
	Pass2               //2線✔︎
	C2                  //2線♣️
	D2                  //2線♦️
	H2                  //2線♥️
	S2                  //2線♠️
	NT2                 //2線♛
	Db2                 //2線✘
	Db2x2               //2線✗✘
	Pass3               //3線✔︎
	C3                  //3線♣️
	D3                  //3線♦️
	H3                  //3線♥️
	S3                  //3線♠️
	NT3                 //3線♛
	Db3                 //3線✘
	Db3x2               //3線✗✘
	Pass4               //4線✔︎
	C4                  //4線♣️
	D4                  //4線♦️
	H4                  //4線♥️
	S4                  //4線♠️
	NT4                 //4線♛
	Db4                 //4線✘
	Db4x2               //4線✗✘
	Pass5               //5線✔︎
	C5                  //5線♣️
	D5                  //5線♦️
	H5                  //5線♥️
	S5                  //5線♠️
	NT5                 //5線♛
	Db5                 //5線✘
	Db5x2               //5線✗✘
	Pass6               //6線✔︎
	C6                  //6線♣️
	D6                  //6線♦️
	H6                  //6線♥️
	S6                  //6線♠️
	NT6                 //6線♛
	Db6                 //6線✘
	Db6x2               //6線✗✘
	Pass7               //7線✔︎
	C7                  //7線♣️
	D7                  //7線♦️
	H7                  //7線♥️
	S7                  //7線♠️
	NT7                 //7線♛
	Db7                 //7線✘
	Db7x2               //7線✗✘
)

var (
	CbCardUint8s = [52]uint8{uint8(Club2), uint8(Club3), uint8(Club4), uint8(Club5), uint8(Club6), uint8(Club7), uint8(Club8), uint8(Club9), uint8(Club10), uint8(ClubJ), uint8(ClubQ), uint8(ClubK), uint8(ClubAce), uint8(Diamond2), uint8(Diamond3), uint8(Diamond4), uint8(Diamond5), uint8(Diamond6), uint8(Diamond7), uint8(Diamond8), uint8(Diamond9), uint8(Diamond10), uint8(DiamondJ), uint8(DiamondQ), uint8(DiamondK), uint8(DiamondAce), uint8(Heart2), uint8(Heart3), uint8(Heart4), uint8(Heart5), uint8(Heart6), uint8(Heart7), uint8(Heart8), uint8(Heart9), uint8(Heart10), uint8(HeartJ), uint8(HeartQ), uint8(HeartK), uint8(HeartAce), uint8(Spade2), uint8(Spade3), uint8(Spade4), uint8(Spade5), uint8(Spade6), uint8(Spade7), uint8(Spade8), uint8(Spade8), uint8(Spade10), uint8(SpadeJ), uint8(SpadeQ), uint8(SpadeK), uint8(SpadeAce)}
	CbBidUint8s  = [56]uint8{uint8(Pass1), uint8(C1), uint8(D1), uint8(H1), uint8(S1), uint8(NT1), uint8(Db1), uint8(Db1x2), uint8(Pass2), uint8(C2), uint8(D2), uint8(H2), uint8(S2), uint8(NT2), uint8(Db2), uint8(Db2x2), uint8(Pass3), uint8(C3), uint8(D3), uint8(H3), uint8(S3), uint8(NT3), uint8(Db3), uint8(Db3x2), uint8(Pass4), uint8(C4), uint8(D4), uint8(H4), uint8(S4), uint8(NT4), uint8(Db4), uint8(Db4x2), uint8(Pass5), uint8(C5), uint8(D5), uint8(H5), uint8(S5), uint8(NT5), uint8(Db5), uint8(Db5x2), uint8(Pass6), uint8(C6), uint8(D6), uint8(H6), uint8(S6), uint8(NT6), uint8(Db6), uint8(Db6x2), uint8(Pass7), uint8(C7), uint8(D7), uint8(H7), uint8(S7), uint8(NT7), uint8(Db7), uint8(Db7x2)}
	//CbSeatUint8s = [4]uint8{east, south, west, north}
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

// GetPartnerByPlayerSeat 以玩家座位,取得夥伴座位
func GetPartnerByPlayerSeat(seat uint8) (uint8, CbSeat) {
	switch CbSeat(seat) {
	case east:
		return uint8(west), west
	case south:
		return uint8(north), north
	case west:
		return uint8(east), east
	case north:
		return uint8(south), south
	}
	return uint8(seatYet), seatYet
}

/*
 pb 與 DDD entity 整合
*/

type (
	RoomUser struct {
		NsConn *skf.NSConn

		*pb.PlayingUser // 坑:要注意,PlayingUser不是用 Reference
		Tracking        Track
		Zone8           uint8 // 從 PlayingUser Zone轉型過來,放在Zone8是為了方便取用
		Bid8            uint8 //叫品
		Play8           uint8 //出牌

		// PlaySeat8 出打出的牌(莊家出莊家的牌或防家出防家的牌,結果Zone8=PlaySeat8)
		// 莊家打出夢家的牌, Zone8=(莊家), PlaySeat8=(夢家)
		PlaySeat8      uint8
		IsClientBroken bool //是否不正常離線(在KickOutBrokenConnection 設定)
	}

	Audiences []*RoomUser //代表非玩家的旁賽者
)

func (ru *RoomUser) Ticket() {
	ru.TicketTime = pb.LocalTimestamp(time.Now())
}
func (ru *RoomUser) TicketString() string {
	return ru.TicketTime.AsTime().Format("01/02 15:04:05")
}

// ToPbUser RoomUser轉換成 pb.PlayingUser (Bid,Play尚未轉換)
func (ru *RoomUser) ToPbUser() *pb.PlayingUser {
	return &pb.PlayingUser{
		Name:       ru.Name,
		Zone:       uint32(ru.Zone8),
		TicketTime: pb.LocalTimestamp(time.Now()),
		Bid:        0,
		Play:       0,
		IsSitting:  ru.IsSitting,
	}
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
	playerSeats = [4]uint8{uint8(east), uint8(south), uint8(west), uint8(north)}
)

// CardRange 區間, 限制該回合能打出牌的範圍
type CardRange [2]uint8

var (

	// CKings 王區間
	CKings CardRange = [2]uint8{club2, clubAce}
	DKings CardRange = [2]uint8{diamond2, diamondAce}
	HKings CardRange = [2]uint8{heart2, heartAce}
	SKings CardRange = [2]uint8{spade2, spadeAce}
	NKings CardRange = [2]uint8{club2, spadeAce}

	// ClubRange 回合跟牌區間(允許出牌區間),若出牌不在區間只有墊牌與切牌兩種可能
	ClubRange    = *(&CKings)
	DiamondRange = *(&DKings)
	HeartRange   = *(&HKings)
	SpadeRange   = *(&SKings)
	TrumpRange   = *(&NKings)
)

// GetTrumpRange 合約底定後,以合約Suit獲取遊戲王牌範圍 (取代 TrumpCardRange)
func GetTrumpRange(contractSuit uint8) CardRange {
	//switch CbSuit(seatBiddingMapperSuit[contractSuit]) {
	switch CbSuit(contractSuit) {
	case CLUB: //0
		return CKings
	case DIAMOND: //1
		return DKings
	case HEART: //2
		return HKings
	case SPADE: //3
		return SKings
	case TRUMP: //4
		return NKings
	default: /*PASS, DOUBLE, REDOUBLE*/
		//這裡應該永遠都不可能執行到
		zeroSuit := uint8(ZeroSuit)
		return [2]uint8{zeroSuit, zeroSuit}
	}
}

// GetRoundRangeByFirstPlay 回合允許出牌的區間,該回合首打(firstPlay)決定所要出牌的花色與區間
func GetRoundRangeByFirstPlay(firstPlay uint8) CardRange {

	//模擬四家的出牌
	// first首打
	var first = CbCard(firstPlay)

	switch {
	case first < Diamond2:
		return ClubRange
	case ClubAce < first && first < Heart2:
		return DiamondRange
	case DiamondAce < first && first < Spade2:
		return HeartRange
	case HeartAce < first && first <= SpadeAce:
		return SpadeRange
	default:
	}
	return [2]uint8{club2, spadeAce}
}

/*
//dartlang中的叫品
	var seat = 0x3 << 6;  //北
	var contract = 0xd;        //S2

	//叫品
	var bidding = seat | contract;
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
	seatBiddingMapperSuit = map[uint8] /*rawBid8*/ uint8 /*Suit*/ {0xC2: 0x0, 0xCA: 0x0, 0xD2: 0x0, 0xDA: 0x0, 0xE2: 0x0, 0xEA: 0x0, 0xF2: 0x0, 0x2: 0x0, 0xA: 0x0, 0x12: 0x0, 0x1A: 0x0, 0x22: 0x0, 0x2A: 0x0, 0x32: 0x0, 0x42: 0x0, 0x4A: 0x0, 0x52: 0x0, 0x5A: 0x0, 0x62: 0x0, 0x6A: 0x0, 0x72: 0x0, 0x82: 0x0, 0x8A: 0x0, 0x92: 0x0, 0x9A: 0x0, 0xA2: 0x0, 0xAA: 0x0, 0xB2: 0x0, 0xC3: 0x1, 0xCB: 0x1, 0xD3: 0x1, 0xDB: 0x1, 0xE3: 0x1, 0xEB: 0x1, 0xF3: 0x1, 0x3: 0x1, 0xB: 0x1, 0x13: 0x1, 0x1B: 0x1, 0x23: 0x1, 0x2B: 0x1, 0x33: 0x1, 0x43: 0x1, 0x4B: 0x1, 0x53: 0x1, 0x5B: 0x1, 0x63: 0x1, 0x6B: 0x1, 0x73: 0x1, 0x83: 0x1, 0x8B: 0x1, 0x93: 0x1, 0x9B: 0x1, 0xA3: 0x1, 0xAB: 0x1, 0xB3: 0x1, 0xC4: 0x2, 0xCC: 0x2, 0xD4: 0x2, 0xDC: 0x2, 0xE4: 0x2, 0xEC: 0x2, 0xF4: 0x2, 0x4: 0x2, 0xC: 0x2, 0x14: 0x2, 0x1C: 0x2, 0x24: 0x2, 0x2C: 0x2, 0x34: 0x2, 0x44: 0x2, 0x4C: 0x2, 0x54: 0x2, 0x5C: 0x2, 0x64: 0x2, 0x6C: 0x2, 0x74: 0x2, 0x84: 0x2, 0x8C: 0x2, 0x94: 0x2, 0x9C: 0x2, 0xA4: 0x2, 0xAC: 0x2, 0xB4: 0x2, 0xC5: 0x3, 0xCD: 0x3, 0xD5: 0x3, 0xDD: 0x3, 0xE5: 0x3, 0xED: 0x3, 0xF5: 0x3, 0x5: 0x3, 0xD: 0x3, 0x15: 0x3, 0x1D: 0x3, 0x25: 0x3, 0x2D: 0x3, 0x35: 0x3, 0x45: 0x3, 0x4D: 0x3, 0x55: 0x3, 0x5D: 0x3, 0x65: 0x3, 0x6D: 0x3, 0x75: 0x3, 0x85: 0x3, 0x8D: 0x3, 0x95: 0x3, 0x9D: 0x3, 0xA5: 0x3, 0xAD: 0x3, 0xB5: 0x3, 0xC6: 0x4, 0xCE: 0x4, 0xD6: 0x4, 0xDE: 0x4, 0xE6: 0x4, 0xEE: 0x4, 0xF6: 0x4, 0x6: 0x4, 0xE: 0x4, 0x16: 0x4, 0x1E: 0x4, 0x26: 0x4, 0x2E: 0x4, 0x36: 0x4, 0x46: 0x4, 0x4E: 0x4, 0x56: 0x4, 0x5E: 0x4, 0x66: 0x4, 0x6E: 0x4, 0x76: 0x4, 0x86: 0x4, 0x8E: 0x4, 0x96: 0x4, 0x9E: 0x4, 0xA6: 0x4, 0xAE: 0x4, 0xB6: 0x4, 0xC7: 0x5, 0xCF: 0x5, 0xD7: 0x5, 0xDF: 0x5, 0xE7: 0x5, 0xEF: 0x5, 0xF7: 0x5, 0x7: 0x5, 0xF: 0x5, 0x17: 0x5, 0x1F: 0x5, 0x27: 0x5, 0x2F: 0x5, 0x37: 0x5, 0x47: 0x5, 0x4F: 0x5, 0x57: 0x5, 0x5F: 0x5, 0x67: 0x5, 0x6F: 0x5, 0x77: 0x5, 0x87: 0x5, 0x8F: 0x5, 0x97: 0x5, 0x9F: 0x5, 0xA7: 0x5, 0xAF: 0x5, 0xB7: 0x5, 0xC8: 0x6, 0xD0: 0x6, 0xD8: 0x6, 0xE0: 0x6, 0xE8: 0x6, 0xF0: 0x6, 0xF8: 0x6, 0x8: 0x6, 0x10: 0x6, 0x18: 0x6, 0x20: 0x6, 0x28: 0x6, 0x30: 0x6, 0x38: 0x6, 0x48: 0x6, 0x50: 0x6, 0x58: 0x6, 0x60: 0x6, 0x68: 0x6, 0x70: 0x6, 0x78: 0x6, 0x88: 0x6, 0x90: 0x6, 0x98: 0x6, 0xA0: 0x6, 0xA8: 0x6, 0xB0: 0x6, 0xB8: 0x6, 0xC1: 0x7, 0xC9: 0x7, 0xD1: 0x7, 0xD9: 0x7, 0xE1: 0x7, 0xE9: 0x7, 0xF1: 0x7, 0x1: 0x7, 0x9: 0x7, 0x11: 0x7, 0x19: 0x7, 0x21: 0x7, 0x29: 0x7, 0x31: 0x7, 0x41: 0x7, 0x49: 0x7, 0x51: 0x7, 0x59: 0x7, 0x61: 0x7, 0x69: 0x7, 0x71: 0x7, 0x81: 0x7, 0x89: 0x7, 0x91: 0x7, 0x99: 0x7, 0xA1: 0x7, 0xA9: 0x7, 0xB1: 0x7}
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
