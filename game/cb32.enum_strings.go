// Code generated by "stringer -type=CbSeat,CbBid,CbCard,CbSuit,Track,CbRole,SeatStatusAndGameStart --linecomment -output cb32.enum_strings.go"; DO NOT EDIT.

package game

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[CbEast-0]
	_ = x[CbSouth-64]
	_ = x[CbWest-128]
	_ = x[CbNorth-192]
}

const (
	_CbSeat_name_0 = "東家"
	_CbSeat_name_1 = "南家"
	_CbSeat_name_2 = "西家"
	_CbSeat_name_3 = "北家"
)

func (i CbSeat) String() string {
	switch {
	case i == 0:
		return _CbSeat_name_0
	case i == 64:
		return _CbSeat_name_1
	case i == 128:
		return _CbSeat_name_2
	case i == 192:
		return _CbSeat_name_3
	default:
		return "CbSeat(" + strconv.FormatInt(int64(i), 10) + ")"
	}
}
func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Pass1-1]
	_ = x[C1-2]
	_ = x[D1-3]
	_ = x[H1-4]
	_ = x[S1-5]
	_ = x[NT1-6]
	_ = x[Db1-7]
	_ = x[Db1x2-8]
	_ = x[Pass2-9]
	_ = x[C2-10]
	_ = x[D2-11]
	_ = x[H2-12]
	_ = x[S2-13]
	_ = x[NT2-14]
	_ = x[Db2-15]
	_ = x[Db2x2-16]
	_ = x[Pass3-17]
	_ = x[C3-18]
	_ = x[D3-19]
	_ = x[H3-20]
	_ = x[S3-21]
	_ = x[NT3-22]
	_ = x[Db3-23]
	_ = x[Db3x2-24]
	_ = x[Pass4-25]
	_ = x[C4-26]
	_ = x[D4-27]
	_ = x[H4-28]
	_ = x[S4-29]
	_ = x[NT4-30]
	_ = x[Db4-31]
	_ = x[Db4x2-32]
	_ = x[Pass5-33]
	_ = x[C5-34]
	_ = x[D5-35]
	_ = x[H5-36]
	_ = x[S5-37]
	_ = x[NT5-38]
	_ = x[Db5-39]
	_ = x[Db5x2-40]
	_ = x[Pass6-41]
	_ = x[C6-42]
	_ = x[D6-43]
	_ = x[H6-44]
	_ = x[S6-45]
	_ = x[NT6-46]
	_ = x[Db6-47]
	_ = x[Db6x2-48]
	_ = x[Pass7-49]
	_ = x[C7-50]
	_ = x[D7-51]
	_ = x[H7-52]
	_ = x[S7-53]
	_ = x[NT7-54]
	_ = x[Db7-55]
	_ = x[Db7x2-56]
}

const _CbBid_name = "1線✔︎1線♣️1線♦️1線♥️1線♠️1線♛1線✘1線✗✘2線✔︎2線♣️2線♦️2線♥️2線♠️2線♛2線✘2線✗✘3線✔︎3線♣️3線♦️3線♥️3線♠️3線♛3線✘3線✗✘4線✔︎4線♣️4線♦️4線♥️4線♠️4線♛4線✘4線✗✘5線✔︎5線♣️5線♦️5線♥️5線♠️5線♛5線✘5線✗✘6線✔︎6線♣️6線♦️6線♥️6線♠️6線♛6線✘6線✗✘7線✔︎7線♣️7線♦️7線♥️7線♠️7線♛7線✘7線✗✘"

var _CbBid_index = [...]uint16{0, 10, 20, 30, 40, 50, 57, 64, 74, 84, 94, 104, 114, 124, 131, 138, 148, 158, 168, 178, 188, 198, 205, 212, 222, 232, 242, 252, 262, 272, 279, 286, 296, 306, 316, 326, 336, 346, 353, 360, 370, 380, 390, 400, 410, 420, 427, 434, 444, 454, 464, 474, 484, 494, 501, 508, 518}

func (i CbBid) String() string {
	i -= 1
	if i >= CbBid(len(_CbBid_index)-1) {
		return "CbBid(" + strconv.FormatInt(int64(i+1), 10) + ")"
	}
	return _CbBid_name[_CbBid_index[i]:_CbBid_index[i+1]]
}
func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[BaseCover-0]
	_ = x[Club2-1]
	_ = x[Club3-2]
	_ = x[Club4-3]
	_ = x[Club5-4]
	_ = x[Club6-5]
	_ = x[Club7-6]
	_ = x[Club8-7]
	_ = x[Club9-8]
	_ = x[Club10-9]
	_ = x[ClubJ-10]
	_ = x[ClubQ-11]
	_ = x[ClubK-12]
	_ = x[ClubAce-13]
	_ = x[Diamond2-14]
	_ = x[Diamond3-15]
	_ = x[Diamond4-16]
	_ = x[Diamond5-17]
	_ = x[Diamond6-18]
	_ = x[Diamond7-19]
	_ = x[Diamond8-20]
	_ = x[Diamond9-21]
	_ = x[Diamond10-22]
	_ = x[DiamondJ-23]
	_ = x[DiamondQ-24]
	_ = x[DiamondK-25]
	_ = x[DiamondAce-26]
	_ = x[Heart2-27]
	_ = x[Heart3-28]
	_ = x[Heart4-29]
	_ = x[Heart5-30]
	_ = x[Heart6-31]
	_ = x[Heart7-32]
	_ = x[Heart8-33]
	_ = x[Heart9-34]
	_ = x[Heart10-35]
	_ = x[HeartJ-36]
	_ = x[HeartQ-37]
	_ = x[HeartK-38]
	_ = x[HeartAce-39]
	_ = x[Spade2-40]
	_ = x[Spade3-41]
	_ = x[Spade4-42]
	_ = x[Spade5-43]
	_ = x[Spade6-44]
	_ = x[Spade7-45]
	_ = x[Spade8-46]
	_ = x[Spade9-47]
	_ = x[Spade10-48]
	_ = x[SpadeJ-49]
	_ = x[SpadeQ-50]
	_ = x[SpadeK-51]
	_ = x[SpadeAce-52]
}

const _CbCard_name = "🀫♣️2♣️3♣️4♣️5♣️6♣️7♣️8♣️9♣️10♣️J♣️Q♣️K♣️A♦️2♦️3♦️4♦️5♦️6♦️7♦️8♦️9♦️10♦️J♦️Q♦️K♦️A♥️2♥️3♥️4♥️5♥️6♥️7♥️8♥️9♥️10♥️J♥️Q♥️K♥️A♠️2♠️3♠️4♠️5♠️6♠️7♠️8♠️9♠️10♠️J♠️Q♠️K♠️A"

var _CbCard_index = [...]uint16{0, 4, 11, 18, 25, 32, 39, 46, 53, 60, 68, 75, 82, 89, 96, 103, 110, 117, 124, 131, 138, 145, 152, 160, 167, 174, 181, 188, 195, 202, 209, 216, 223, 230, 237, 244, 252, 259, 266, 273, 280, 287, 294, 301, 308, 315, 322, 329, 336, 344, 351, 358, 365, 372}

func (i CbCard) String() string {
	if i >= CbCard(len(_CbCard_index)-1) {
		return "CbCard(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _CbCard_name[_CbCard_index[i]:_CbCard_index[i+1]]
}
func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[CLUB-0]
	_ = x[DIAMOND-1]
	_ = x[HEART-2]
	_ = x[SPADE-3]
	_ = x[TRUMP-4]
	_ = x[DOUBLE-5]
	_ = x[REDOUBLE-6]
	_ = x[PASS-7]
}

const _CbSuit_name = "♣️♦️♥️♠️👑👩\u200d👦👩\u200d👩\u200d👧\u200d👦👀PASS"

var _CbSuit_index = [...]uint8{0, 6, 12, 18, 24, 28, 39, 64, 72}

func (i CbSuit) String() string {
	if i >= CbSuit(len(_CbSuit_index)-1) {
		return "CbSuit(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _CbSuit_name[_CbSuit_index[i]:_CbSuit_index[i+1]]
}
func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[IddleTrack-0]
	_ = x[EnterRoom-1]
	_ = x[LeaveRoom-2]
	_ = x[EnterGame-3]
	_ = x[LeaveGame-4]
}

const _Track_name = "無法追蹤,enum暫時沒用進入房間(或離開遊戲)離開房間 (前端觸動)進入遊戲(或從房間進入)離開遊戲 (前端觸動)"

var _Track_index = [...]uint8{0, 29, 58, 85, 117, 144}

func (i Track) String() string {
	if i < 0 || i >= Track(len(_Track_index)-1) {
		return "Track(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _Track_name[_Track_index[i]:_Track_index[i+1]]
}
func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[RoleNotYet-0]
	_ = x[Audience-1]
	_ = x[Defender-2]
	_ = x[Declarer-3]
	_ = x[Dummy-4]
}

const _CbRole_name = "競叫尚未底定👨\u200d👨\u200d👧\u200d👧🙅🏻\u200d♂️🥷🏻🙇🏼"

var _CbRole_index = [...]uint8{0, 18, 43, 60, 68, 76}

func (i CbRole) String() string {
	if i >= CbRole(len(_CbRole_index)-1) {
		return "CbRole(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _CbRole_name[_CbRole_index[i]:_CbRole_index[i+1]]
}
func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[SeatGameNA-0]
	_ = x[SeatFullBecauseGameStart-1]
	_ = x[SeatGetButGameWaiting-2]
	_ = x[SeatGetAndStartGame-3]
}

const _SeatStatusAndGameStart_name = "SeatGameNASeatFullBecauseGameStartSeatGetButGameWaitingSeatGetAndStartGame"

var _SeatStatusAndGameStart_index = [...]uint8{0, 10, 34, 55, 74}

func (i SeatStatusAndGameStart) String() string {
	if i >= SeatStatusAndGameStart(len(_SeatStatusAndGameStart_index)-1) {
		return "SeatStatusAndGameStart(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _SeatStatusAndGameStart_name[_SeatStatusAndGameStart_index[i]:_SeatStatusAndGameStart_index[i+1]]
}
