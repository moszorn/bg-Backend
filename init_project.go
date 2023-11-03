package project

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/moszorn/utils/skf"
	"project/game"
)

var (
	shortConnID = func(c *skf.NSConn) string {
		var (
			index = strings.LastIndex(c.String(), "-")
			id    = c.String()[index+1:]
		)
		if c.Conn.IsClosed() {
			return "斷線⛓️" + id
		}
		return id
	}

	roomLog = func(c *skf.NSConn, msg skf.Message) {
		slog.Debug("🏠",
			slog.String("room", msg.Room),
			slog.String("connId", shortConnID(c)),
			slog.String("event", msg.Event),
		)
	}

	generalLog = func(c *skf.NSConn, msg skf.Message) {

		var (
			namespace string = "空值"
			room      string = "空值"
			event     string = "空值"
		)

		shotId := fmt.Sprintf("( %s )", shortConnID(c))
		if msg.Namespace != "" {
			namespace = msg.Namespace
		}
		if msg.Room != "" {
			room = msg.Room
		}
		if msg.Event != "" {
			event = msg.Event
		}

		slog.Debug("一般日誌",
			slog.String("space", namespace),
			slog.String("event", event),
			slog.String("room", room),
			slog.String("connId", shotId))

		if 0 < len(msg.Body) {
			slog.Debug("一般日誌", slog.String("Body", string(msg.Body)))
		}
		slog.Debug("⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺⎺ ")
	}

	GameConst = game.GameConstantExport()
)

// InitProject 必須由 main呼叫
func InitProject(pid context.Context) {
	initNamespace(pid)
}
