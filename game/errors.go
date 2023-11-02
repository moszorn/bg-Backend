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

/*============================================================================================*/
// App 錯誤定義

// TODO 轉成 Proto Message

// AppCode 表示專案相關碼,碼不限提示,警告,錯誤
type AppCode byte

const (
	AppCodeZero  AppCode = iota // 保留
	ApplicationC                //一般應用程式有關, ,可能是Bud,錯誤
	BroadcastC                  //廣播有,可能是Bud,錯誤
	GamingC                     // 遊戲有,可能是Bud,錯誤
	RoomSeatC                   //房間或遊戲座位有關,可能是Bud,錯誤
	NSConnC                     //連線有關,可能是Bud,錯誤
	SystemC                     //系統有關,可能是Bud,錯誤
	InfraC                      // AWS有關, ex:EC2 錯誤

)

// TODO 轉成 Proto Message

type (
	AppErr struct {
		reason interface{}
		Err    error  //當全部出問題,或本身就是錯誤時使用 Err
		Msg    string //不是全部失敗,只有少數失敗,用Msg提示訊息
		Code   AppCode
	}
)
