package project

import "fmt"

type BackendCode uint8

const (
	BackendCodeZero    BackendCode = iota // 保留
	GeneralCode                           //一般應用程式有關, ,可能是Bud,錯誤
	RoomClientCode                        //房間或遊戲座位有關,可能是Bud,錯誤
	ConnectionCode                        //連線有關,可能是Bud,錯誤
	SystemCode                            //系統有關,可能是Bud,錯誤
	InfrastructureCode                    // AWS有關, ex:EC2 錯誤
)

type (
	BackendErr struct {
		reason interface{}
		Err    error  //當全部出問題,或本身就是錯誤時使用 Err
		Msg    string //不是全部失敗,只有少數失敗,用Msg提示訊息
		Code   BackendCode
	}
)

func (appErr *BackendErr) Error() string {
	return fmt.Sprintf("%d: %s", appErr.Code, appErr.Msg)
}

func BackendError(code BackendCode, msg string, reason interface{}) (err *BackendErr) {
	err = new(BackendErr)
	err.Msg = msg
	err.Code = code
	err.reason = reason
	return
}
