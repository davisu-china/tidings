// Package ws 是站内实时通道（§4.7 的「实时收发」）。
//
// 只下行：发消息仍然走 HTTP POST。这不是偷懒，是因为发消息需要的
// 东西这一层一个都没有 —— 鉴权中间件、内容校验、幂等键、错误码，
// 全都要在 WebSocket 上再实现一遍，而两套实现迟早会不一致（一边
// 挡了空消息、另一边没挡）。收消息则天然适合长连接：它是「别人做了
// 什么」的推送，不需要请求-响应配对，也就不需要错误码。
//
// 跨实例靠 Redis pub/sub（§10.6 的 ws:fanout）。本进程既是发布者也是
// 订阅者：Publish 只往 Redis 发，由本地的订阅循环投给连在本实例上的
// 连接。绕一圈 Redis 而不是「本地直投 + 顺便广播」，是为了让投递
// 只有一条路径 —— 有两条就一定会有人收到两份。
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
)

// Event 是推给前端的一条事件。Data 里放什么由业务决定，这一层不解释。
type Event struct {
	Type string
	Data map[string]any
}

// 事件类型。
//
// 只有两种。会话列表的未读数、引荐的未读角标都不走这里：它们由
// 前端在收到事件后重新拉一次接口（消息与匹配都改了服务端状态，
// 让推送去猜新的数字迟早会猜错）。
const (
	// EventMessage 会话里来了一条新消息。
	EventMessage = "message"
	// EventMatch 一条引荐成了匹配，会话被创建出来。
	EventMatch = "match"
)

// fanoutChannel 是跨实例广播用的频道名，取自 §10.6 的连接表。
const fanoutChannel = "ws:fanout"

const (
	// sendBuffer 是一个连接最多积压几帧。
	//
	// 积压说明这个客户端读得慢（切到后台的标签页、断网但还没超时的
	// 手机）。满了就丢帧而不是阻塞投递方 —— 一条通知卡住所有人的
	// 代价远大于某个慢客户端少收一条：他下次拉列表就对上了。
	sendBuffer = 32

	// writeTimeout 是一次写（或一次 ping）的上限。超过就当作这条
	// 连接已经坏了。
	writeTimeout = 10 * time.Second

	// pingInterval 是心跳间隔。
	//
	// 应用层心跳防的是反向代理的空闲超时（nginx 的 proxy_read_timeout
	// 默认 60 秒）：连接看着是好的，其实中间那一跳早就断了，表现是
	// 「消息发出去对方永远收不到」。TCP keepalive 的默认间隔是小时级，
	// 在这件事上帮不上忙。
	pingInterval = 30 * time.Second
)

// envelope 是过 Redis 的信封。带上 user_id 是因为订阅端拿到的是
// 所有人混在一起的一条流，得知道该投给谁。
type envelope struct {
	UserID int64           `json:"user_id"`
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// frame 是发给浏览器的形状。与信封分开：信封是内部约定，
// 前端只该看见 type 和 data。
type frame struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Hub 管着本实例上的所有连接。
type Hub struct {
	mu    sync.Mutex
	conns map[int64]map[*client]struct{}

	rdb *redis.Client
	log *slog.Logger

	ready     chan struct{}
	readyOnce sync.Once
}

// NewHub 建一个 Hub。rdb 为 nil 时 Publish 直接返回错误（实时通道
// 不是必需品，缺了它业务照走）。
func NewHub(rdb *redis.Client, log *slog.Logger) *Hub {
	return &Hub{
		conns: make(map[int64]map[*client]struct{}),
		rdb:   rdb,
		log:   log,
		ready: make(chan struct{}),
	}
}

// Ready 在订阅确认之后关闭。
//
// 启动时等它一下是有必要的：Subscribe 只把命令写出去就返回了，
// 不等 Redis 的确认。不确认就开始服务，进程刚起来那几微秒里发出的
// 通知会直接丢掉，而且没有任何痕迹。
func (h *Hub) Ready() <-chan struct{} { return h.ready }

// Publish 把一条事件发给某个用户，投递由各实例的订阅循环完成。
func (h *Hub) Publish(ctx context.Context, uid int64, ev Event) error {
	if h.rdb == nil {
		return nil
	}
	data, err := json.Marshal(ev.Data)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(envelope{UserID: uid, Type: ev.Type, Data: data})
	if err != nil {
		return err
	}
	return h.rdb.Publish(ctx, fanoutChannel, raw).Err()
}

// Run 订阅广播频道并投递，直到 ctx 结束。
//
// 出错不退出：Redis 抖一下、连接被中间设备掐掉，都只该让投递停几秒。
// 这个循环死了的后果是所有实时推送静默失效 —— 没有报错、没有日志，
// 只有用户觉得「消息怎么不来了」。
func (h *Hub) Run(ctx context.Context) {
	for {
		err := h.subscribeLoop(ctx)
		if ctx.Err() != nil {
			return
		}
		h.log.ErrorContext(ctx, "实时通道订阅中断，3 秒后重连", slog.Any("err", err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (h *Hub) subscribeLoop(ctx context.Context) error {
	sub := h.rdb.Subscribe(ctx, fanoutChannel)
	defer func() { _ = sub.Close() }()

	for {
		msg, err := sub.Receive(ctx)
		if err != nil {
			return err
		}
		switch m := msg.(type) {
		case *redis.Subscription:
			h.readyOnce.Do(func() {
				close(h.ready)
				h.log.Info("实时通道已订阅", slog.String("channel", fanoutChannel))
			})
		case *redis.Message:
			h.dispatch([]byte(m.Payload))
		}
	}
}

// dispatch 解析一条广播并投给本实例上的连接。
func (h *Hub) dispatch(raw []byte) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		// 频道上出现了解析不了的东西：要么是别的程序用了同名频道，
		// 要么是本程序的版本不一致。记一条就够了，不要让它打断循环。
		h.log.Warn("实时通道收到无法解析的消息", slog.Any("err", err))
		return
	}
	out, err := json.Marshal(frame{Type: env.Type, Data: env.Data})
	if err != nil {
		h.log.Warn("实时事件编码失败", slog.Any("err", err))
		return
	}

	h.mu.Lock()
	targets := make([]*client, 0, len(h.conns[env.UserID]))
	for c := range h.conns[env.UserID] {
		targets = append(targets, c)
	}
	h.mu.Unlock()

	for _, c := range targets {
		select {
		case c.send <- out:
		default:
			h.log.Warn("实时通道积压，丢弃一帧",
				slog.Int64("user_id", env.UserID), slog.String("type", env.Type))
		}
	}
}

// ---------------------------------------------------------------- 连接

// client 是一条连接。同一用户可能有多条（多个标签页、手机和电脑同时开着）。
type client struct {
	uid  int64
	conn *websocket.Conn
	// send 只由 dispatch 写入、只由该连接的 writeLoop 读出。
	// 它永远不会被 close —— 关掉它就必须保证没有人在写，
	// 而「没有人在写」这件事在并发下无法廉价地保证，
	// 关错了就是 panic: send on closed channel。
	send chan []byte
}

// Serve 接管一条已经鉴权过的连接，直到它断开。它会阻塞当前
// HTTP 处理函数，这是长连接服务的正常形态。
//
// uid 由调用方（handler）鉴权后传入 —— 这一层不做鉴权，
// 它只认「这条连接属于谁」。
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, uid int64) {
	// opts 传 nil：库自带的同源校验就会生效（Origin 的 host 必须
	// 等于请求的 Host）。留着它，跨站页面就没法用受害者的 cookie
	// 连上我们的实时通道。要放开跨域得显式写 OriginPatterns，
	// 那种时候应当先想清楚为什么。
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept 在所有错误路径上都自己写过 HTTP 响应了
		h.log.WarnContext(r.Context(), "WebSocket 握手失败",
			slog.Int64("user_id", uid), slog.Any("err", err))
		return
	}

	c := &client{uid: uid, conn: conn, send: make(chan []byte, sendBuffer)}

	// CloseRead 起一个 goroutine 把连接读空到断开为止，并保证
	// ping / pong / close 这些控制帧被正常应答。没有它，连接不会
	// 回应任何控制帧，对端（和中间的代理）会把它当成死连接清掉。
	//
	// 它同时也是 c.Ping 能工作的前提：Ping 不从连接上读，它等的是
	// 读循环读到 pong。
	//
	// 客户端发来数据帧会被它按协议违规关掉 —— 这个通道只下行，
	// 客户端不该发任何东西。
	ctx := conn.CloseRead(r.Context())

	h.add(c)
	defer func() {
		h.drop(c)
		// 已经关掉的连接会返回错误，忽略即可
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	c.writeLoop(ctx)
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[c.uid] == nil {
		h.conns[c.uid] = make(map[*client]struct{})
	}
	h.conns[c.uid][c] = struct{}{}
}

// drop 摘掉一条连接。重复调用是安全的（同一连接只会被 defer 调一次，
// 但 CloseRead 的取消与写失败可能同时发生，两处都想摘）。
func (h *Hub) drop(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.conns[c.uid]
	delete(set, c)
	if len(set) == 0 {
		delete(h.conns, c.uid)
	}
}

// writeLoop 是这条连接上唯一写连接的地方。
//
// 一次只能有一个写者 —— 并发写 WebSocket 会把帧交叠在一起，
// 对端解析出来的就是乱码或错误。把写收在一个 goroutine 里，
// 是唯一不用加锁的做法。
func (c *client) writeLoop(ctx context.Context) {
	tick := time.NewTicker(pingInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := c.ping(ctx); err != nil {
				return
			}
		case out := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(wctx, websocket.MessageText, out)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (c *client) ping(ctx context.Context) error {
	pctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.conn.Ping(pctx)
}
