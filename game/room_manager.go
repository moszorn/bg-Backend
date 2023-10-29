package game

import (
	"log/slog"
	"time"

	"github.com/moszorn/utils/rchanr"
	"github.com/moszorn/utils/skf"
)

// RoomManager 管理進入房間的所有使用者,包含廣播所有房間使用者,
// 未來可能會分方位(RoomZorn),禁言,聊天可能都透過RoomManager
type (
	chanRepCode uint8
	chanResult  struct {
		// 尚為用到
		code        chanRepCode
		seat        *uint8
		isGameStart bool
		err         error
	}

	ZoneUsers     map[*skf.NSConn]*RoomUser
	RoomZoneUsers map[uint8]ZoneUsers
	RoomManager   struct {
		door         rchanr.ChanReqWithArguments[*RoomUser, *chanResult]
		broadcastMsg rchanr.ChanReqWithArguments[*skf.Message, []*RoomUser]

		Users    RoomZoneUsers
		ticketSN int //目前房間人數流水號,從1開始
	}
)

func NewRoomManager() *RoomManager {
	//Zone
	roomZoneUsers := make(map[uint8]ZoneUsers)

	//make Zone
	for idx := range playerSeats {
		roomZoneUsers[playerSeats[idx]] = make(map[*skf.NSConn]*RoomUser)
	}

	return &RoomManager{
		Users:        roomZoneUsers,
		door:         make(chan rchanr.ChanRepWithArguments[*RoomUser, *chanResult]),
		broadcastMsg: make(chan rchanr.ChanRepWithArguments[*skf.Message, []*RoomUser]),
	}
}

func (mr *RoomManager) StartLoop() {
	for {
		select {
		//坑: 這裡只能針對 gateway channel
		case tracking := <-mr.door:
			user := tracking.Question
			switch user.Tracking {
			case EnterRoom:
				if _, exist := mr.getRoomUser(user.NsConn, user.Zone); exist {
					tracking.Response <- &chanResult{
						err: ErrUserInRoom,
					}
				}
				if mr.ticketSN > RoomUsersLimit {
					tracking.Response <- &chanResult{
						err: ErrRoomFull,
					}
				} else {

					//房間進入者流水編號累增
					mr.ticketSN++
					user.Ticket()

					// 玩家加入遊戲房間
					mr.Users[user.Zone][user.NsConn] = user
					tracking.Response <- nil
				}
			case LeaveRoom:

				// 離開玩家
				delete(mr.Users[user.Zone], user.NsConn)

				//TBC 底下一行離開房間,到大廳,是否需要斷線
				// user.NsConn = nil

				//房間進入者流水編號遞減
				mr.ticketSN--

				user = nil

				tracking.Response <- nil

			case EnterGame:
				tracking.Response <- mr.playerJoin(user)
			case LeaveGame:
				tracking.Response <- mr.playerLeave(user)
			}

		case send := <-mr.broadcastMsg:
			msg := send.Question
			send.Response <- mr.broadcast(msg)
		default:
			// 移除突然斷線的user
			//g.rmClosedUsers()

		}
	}
}

// getRoomUser used in multi thread
func (mr *RoomManager) getRoomUser(nsconn *skf.NSConn, zone uint8) (found *RoomUser, isExist bool) {
	found, isExist = mr.Users[zone][nsconn]
	return found, isExist
}

func (mr *RoomManager) UserJoin(ns *skf.NSConn, userName string, userZone uint8) (chanSeat *chanResult) {
	user := &RoomUser{
		NsConn:     ns,
		Name:       userName,
		TicketTime: time.Time{},
		Tracking:   0,
		Zone:       userZone,
	}
	preTracking := user.Tracking
	user.Tracking = EnterRoom

	//Probe內部用user name查詢是否user已經入房間
	chanSeat = mr.door.Probe(user)
	if chanSeat == nil {
		//入房成功
		return
	}

	// 房間已滿
	if chanSeat.err != nil {
		//還原Tracking
		user.Tracking = preTracking
	}
	return
}
func (mr *RoomManager) UserLeave(ns *skf.NSConn, userName string, userZone uint8) (chanSeat *chanResult) {

	user := &RoomUser{
		NsConn:     ns,
		Name:       userName,
		TicketTime: time.Time{},
		Tracking:   0,
		Zone:       userZone,
	}
	preTracking := user.Tracking
	user.Tracking = LeaveRoom

	if chanSeat = mr.door.Probe(user); chanSeat.err != nil {
		user.Tracking = preTracking
	}
	return nil
}

func (mr *RoomManager) roomInfo() {
	//Total: 每個Zone人數相加
	eastZone := len(mr.Users[playerSeats[0]])
	southZone := len(mr.Users[playerSeats[1]])
	westZone := len(mr.Users[playerSeats[2]])
	northZone := len(mr.Users[playerSeats[3]])
	total := eastZone + southZone + westZone + northZone
	slog.Info("房間資訊",
		slog.Int("East人數", eastZone),
		slog.Int("South人數", southZone),
		slog.Int("West人數", westZone),
		slog.Int("North人數", northZone),
		slog.Int("房間總人數", total))
}
