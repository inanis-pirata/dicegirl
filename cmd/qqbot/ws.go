package main

// WebSocket gateway connection: Hello → Identify/Resume → heartbeat → event
// dispatch, with automatic resume and exponential-backoff reconnect. The
// protocol surface is tiny (opcodes 10/2/0/1/11/6/7/9); see
// bot.q.qq.com/wiki/develop/api-v2/dev-prepare/event-emit/.

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	opDispatch      = 0
	opHeartbeat     = 1
	opIdentify      = 2
	opResume        = 6
	opReconnect     = 7
	opInvalid       = 9
	opHello         = 10
	opHeartbeatAck  = 11
	wsWriteWait     = 10 * time.Second
	wsReconnectBase = 2 * time.Second
	wsReconnectMax  = 60 * time.Second
)

// fatalCloseCodes end the process: 4013 invalid intent, 4014 intent not
// permitted (the bot lacks the group/C2C capability in the admin console).
var fatalCloseCodes = map[int]bool{4013: true, 4014: true}

// noResumeCloseCodes force a fresh Identify instead of Resume.
var noResumeCloseCodes = map[int]bool{4006: true, 4007: true, 4013: true, 4014: true}

type wsPayload struct {
	Op   int             `json:"op"`
	Data json.RawMessage `json:"d,omitempty"`
	Seq  *int64          `json:"s,omitempty"`
	Type string          `json:"t,omitempty"`
}

// handler processes one decoded message event.
type handler func(ctx context.Context, scope string, ev messageEvent)

type gateway struct {
	api     *apiClient
	intents int
	onEvent handler
	logger  *log.Logger
	debug   bool

	// session state, guarded across reconnects
	stateMu sync.Mutex
	session string
	seq     int64
	haveSeq bool
}

func newGateway(api *apiClient, onEvent handler, logger *log.Logger, debug bool) *gateway {
	return &gateway{api: api, intents: IntentGroupAndC2C, onEvent: onEvent, logger: logger, debug: debug}
}

// run maintains the connection until ctx is canceled. Fatal protocol
// errors (bad intents) are returned; transient failures loop forever with
// backoff because a dice bot must self-heal across network restarts.
func (g *gateway) run(ctx context.Context) error {
	backoff := wsReconnectBase
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		g.logger.Print("获取 access_token…")
		if _, err := g.api.accessToken(ctx); err != nil {
			g.logger.Printf("获取 access_token 失败：%v", err)
			if !sleep(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff)
			continue
		}
		g.logger.Print("access_token 获取成功，查询 gateway 地址…")
		url, err := g.api.GatewayURL(ctx)
		if err != nil {
			g.logger.Printf("获取 gateway 失败：%v", err)
			if !sleep(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff)
			continue
		}
		g.logger.Printf("连接网关 %s", sanitizeURL(url))
		code, err := g.serveConn(ctx, url)
		if ctx.Err() != nil {
			return nil
		}
		if code != 0 && fatalCloseCodes[code] {
			return errFatalGateway(code)
		}
		if code != 0 && noResumeCloseCodes[code] {
			g.resetSession()
		}
		g.logger.Printf("连接断开（code=%d, err=%v），%s 后重连", code, err, backoff)
		if !sleep(ctx, backoff) {
			return nil
		}
		backoff = nextBackoff(backoff)
	}
}

func errFatalGateway(code int) error {
	return &gatewayFatal{code: code}
}

type gatewayFatal struct{ code int }

func (e *gatewayFatal) Error() string {
	switch e.code {
	case 4013:
		return "网关拒绝：intent 无效（4013），请检查机器人管理端能力配置"
	case 4014:
		return "网关拒绝：intent 无权限（4014），请在管理端开通群聊/消息列表能力"
	default:
		return "网关致命错误（close code）"
	}
}

// serveConn drives one WebSocket connection and returns its close code.
func (g *gateway) serveConn(ctx context.Context, url string) (int, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		// The platform upgrades from https/wss; standard headers suffice.
	}
	conn, _, err := dialer.DialContext(ctx, url, http.Header{})
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	var writeMu sync.Mutex
	writeJSON := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		return conn.WriteJSON(v)
	}

	// 1) Wait for Hello.
	conn.SetReadDeadline(time.Now().Add(wsWriteWait))
	var hello wsPayload
	if err := conn.ReadJSON(&hello); err != nil {
		return 0, err
	}
	if hello.Op != opHello {
		return 0, &gatewayFatal{code: 0}
	}
	var hi helloData
	if err := json.Unmarshal(hello.Data, &hi); err != nil {
		return 0, err
	}
	interval := time.Duration(hi.HeartbeatInterval) * time.Millisecond
	if interval <= 0 {
		interval = 45 * time.Second
	}

	token, err := g.api.accessToken(ctx)
	if err != nil {
		return 0, err
	}

	// 2) Resume a live session or Identify fresh.
	g.stateMu.Lock()
	session, seq, haveSeq := g.session, g.seq, g.haveSeq
	g.stateMu.Unlock()
	if session != "" {
		if err := writeJSON(wsPayload{Op: opResume, Data: mustJSON(resumeData{
			Token: "QQBot " + token, SessionID: session, Seq: seq,
		})}); err != nil {
			g.resetSession()
		}
		g.logger.Printf("已请求 Resume（session=%s, seq=%d）", session, seq)
	} else {
		if err := writeJSON(wsPayload{Op: opIdentify, Data: mustJSON(identifyData{
			Token: "QQBot " + token, Intents: g.intents, Shard: [2]int{0, 1},
		})}); err != nil {
			return 0, err
		}
	}

	// 3) Heartbeat loop; a missed ack forces reconnect.
	hbCtx, stopHB := context.WithCancel(ctx)
	defer stopHB()
	var (
		hbMu   sync.Mutex
		acked  = true
		hbDone = make(chan struct{})
	)
	go func() {
		defer close(hbDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				hbMu.Lock()
				healthy := acked
				acked = false
				g.stateMu.Lock()
				var d any
				if haveSeq {
					s := g.seq
					d = &s
				}
				g.stateMu.Unlock()
				hbMu.Unlock()
				if !healthy {
					g.logger.Print("心跳未收到 ACK，主动重连")
					_ = conn.Close()
					return
				}
				if err := writeJSON(wsPayload{Op: opHeartbeat, Data: mustJSON(d)}); err != nil {
					return
				}
			}
		}
	}()

	// 4) Read/dispatch loop. Bounded worker pool keeps a slow command from
	// blocking event delivery while still capping concurrency.
	const workers = 8
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn.SetReadDeadline(time.Time{}) // heartbeats prove liveness
		var env wsPayload
		if err := conn.ReadJSON(&env); err != nil {
			stopHB()
			<-hbDone
			if ce, ok := err.(*websocket.CloseError); ok {
				return ce.Code, err
			}
			return 0, err
		}
		switch env.Op {
		case opHeartbeat:
			// Server-requested immediate heartbeat.
			g.stateMu.Lock()
			var d any
			if g.haveSeq {
				s := g.seq
				d = &s
			}
			g.stateMu.Unlock()
			_ = writeJSON(wsPayload{Op: opHeartbeat, Data: mustJSON(d)})
		case opHeartbeatAck:
			hbMu.Lock()
			acked = true
			hbMu.Unlock()
		case opReconnect:
			stopHB()
			_ = conn.Close()
			return 0, nil
		case opInvalid:
			g.logger.Print("收到 Invalid Session，丢弃会话后重新 Identify")
			g.resetSession()
		case opDispatch:
			if env.Seq != nil {
				g.stateMu.Lock()
				g.seq = *env.Seq
				g.haveSeq = true
				g.stateMu.Unlock()
			}
			if g.debug && (env.Type == eventGroupAtMessage || env.Type == eventC2CMessage) {
				g.logger.Printf("事件原文 type=%s seq=%d data=%s",
					env.Type, g.seq, truncate(string(env.Data), 800))
			}
			switch env.Type {
			case "READY":
				var rd readyData
				if err := json.Unmarshal(env.Data, &rd); err == nil && rd.SessionID != "" {
					g.stateMu.Lock()
					g.session = rd.SessionID
					g.stateMu.Unlock()
					g.logger.Printf("READY：机器人 %s（%s），session=%s",
						rd.User.Name, rd.User.ID, rd.SessionID)
				}
			case "RESUMED":
				g.logger.Print("RESUMED：断线期间事件已补推")
			case eventGroupAtMessage, eventC2CMessage:
				var me messageEvent
				if err := json.Unmarshal(env.Data, &me); err != nil {
					g.logger.Printf("解析事件失败：%v", err)
					continue
				}
				scope := botScopeGroup
				if env.Type == eventC2CMessage {
					scope = botScopeC2C
				}
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return 0, ctx.Err()
				}
				wg.Add(1)
				go func(me messageEvent) {
					defer wg.Done()
					defer func() { <-sem }()
					defer func() {
						if r := recover(); r != nil {
							g.logger.Printf("事件处理 panic：%v", r)
						}
					}()
					g.onEvent(ctx, scope, me)
				}(me)
			default:
				// Lifecycle events (GROUP_ADD_ROBOT, FRIEND_ADD_ROBOT, …) and
				// anything newer than this build: log them so a silent platform
				// side never looks like "the bot did nothing".
				g.logger.Printf("未处理事件 type=%s data=%s", env.Type, truncate(string(env.Data), 300))
			}
		}
	}
}

func (g *gateway) resetSession() {
	g.stateMu.Lock()
	g.session = ""
	g.seq = 0
	g.haveSeq = false
	g.stateMu.Unlock()
}

// mustJSON marshals v to RawMessage, falling back to null. Inputs here are
// fixed local structs (or nil), so a marshal failure is a programmer error.
func mustJSON(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage("null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > wsReconnectMax {
		d = wsReconnectMax
	}
	// Full jitter avoids reconnect synchronization after outages.
	return time.Duration(rand.Int63n(int64(d)/2 + int64(d)/2))
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// sanitizeURL drops any query parameters before logging (gateway URLs may
// carry per-connection tokens).
func sanitizeURL(u string) string {
	for i := 0; i < len(u); i++ {
		if u[i] == '?' {
			return u[:i]
		}
	}
	return u
}
