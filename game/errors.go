package game

import "errors"

var (
	ErrRoomFull         = errors.New("房間人數上限,無法再進入")
	ErrUserInRoom       = errors.New("玩家已經在房間")
	ErrUserInPlay       = errors.New("玩家正在遊戲中")
	ErrUserNotFound     = errors.New("玩家不存在")
	ErrUserNotInPlay    = errors.New("玩家已離開遊戲,或不在遊戲中")
	ErrPlayMultipleGame = errors.New("不能同時多局遊戲")
	ErrGameSeatFull     = errors.New("遊戲桌已滿,你晚了一步")
	ErrGameStart        = errors.New("遊戲已經開始")
)

var (
	ErrBiddingInvalid = errors.New("叫品不合法")
)
