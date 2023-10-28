package main

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/kataras/neffos"

	utilog "github.com/moszorn/utils/log"
	"github.com/moszorn/utils/skf"
	"github.com/moszorn/utils/skf/gobwas"

	"project"
)

var (
	//Inject from Makefile (listen port)
	endPort string

	pid    = strconv.Itoa(os.Getpid())
	cpuNum = runtime.NumCPU()
)

func init() {
	runtime.GOMAXPROCS(cpuNum)
	slog.Info("cpu核心數", slog.Int("Core", cpuNum))
}

func main() {
	slog.Debug("錯誤訊息測試", utilog.Err(errors.New("hello world")))

	ctrl := make(chan os.Signal)
	signal.Notify(ctrl, os.Kill, os.Interrupt)

	project.InitProject()

	go gameServerStart()

	<-ctrl

	err := exec.Command("kill", pid, "-9", "-v").Start()

	slog.Info("Shut Down Contract Bridge Game", slog.String("pid", pid))

	if err != nil {
		slog.Error("kill process fail", utilog.Err(err), slog.String("pid", pid))
	}
}

func gameServerStart() {

	server := skf.New(gobwas.DefaultUpgrader, project.Namespace)
	slog.Debug("設定server", slog.Bool("namespace", true))

	//server.IDGenerator = Id
	//server.OnConnect = OnConnect
	//server.OnDisconnect = OnDisconnect
	//server.OnUpgradeError = 尚未實作

	//定時跑馬燈
	//go periodMarquee(server)

	slog.Info("Contract Bridge Game", slog.String("pid", pid))
	slog.Debug("Ctrl-C中斷Server執行")
	err := http.ListenAndServe(endPort, server)
	if err != nil {
		slog.Error("server 啟動失敗", utilog.Err(err))
	}

}

// Id 連線 ID生成器
func Id(w http.ResponseWriter, r *http.Request) string {
	if uid := r.Header.Get("X-Username"); uid != "" {
		return uid
	}
	return neffos.DefaultIDGenerator(w, r)
}

func OnConnect(c *skf.Conn) error {
	var (
		idx = strings.LastIndex(c.ID(), "-")
		id  = c.String()[idx+1:]
	)
	slog.Debug("serverEvent", slog.String("event", "OnConnect"), slog.String("id", id))

	//這可以對當前連線,個別送出訊息,如下
	// ns, err := c.Connect(context.Background(), nameSpace)
	// ns.Emit(eventName, []byte("歡迎光臨"))

	//也可以除了當前連線外的,Server廣播
	// c.serverEvent().Broadcast(c, msg)

	return nil
}

func OnDisconnect(c *skf.Conn) {
	var (
		idx = strings.LastIndex(c.ID(), "-")
		id  = c.String()[idx+1:]
	)
	slog.Debug("serverEvent", slog.String("event", "OnDisconnect"), slog.String("id", id))
}

func periodMarquee(server *skf.Server) {

	var (
		msgBuf       = bytes.NewBuffer(make([]byte, 1024))
		spaceDelim   = byte(32)
		msg          = []byte("大廳公告")
		announcement []byte
		payload      skf.Message
	)

	msgBuf.Write(msg)
	announcement, _ = msgBuf.ReadBytes(spaceDelim)

	payload = skf.Message{
		Namespace: "xxxxSpace", /* game.LobbySpaceName */
		Event:     "xxxx",
		Body:      announcement,
		SetBinary: false,
	}

	//讀取後清空快取訊息
	msgBuf.Truncate(0)

	//10秒後公告發布
	time.Sleep(time.Second * 10)
	server.Broadcast(nil, payload)

	//之後每分鐘發佈一次現在Server時間
	for {
		time.Sleep(time.Minute)

		msgBuf.Write([]byte(time.Now().Format("15:04:05")))
		payload.Body, _ = msgBuf.ReadBytes(spaceDelim)

		server.Broadcast(nil, payload)
	}
}
