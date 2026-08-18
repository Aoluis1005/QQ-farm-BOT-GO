// Package gw 腾讯农场网关 WebSocket 客户端
package gw

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"

	"github.com/Aoluis1005/go-farm-bot/ace"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// Node 默认微信 UA
// iOS Safari UA（wx+iOS 唯一可握手的 UA）
const defaultUserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 26_2_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

// maxQueued 单个连接等待槽位的在途请求上限（对齐参考实现）
const maxQueued = int64(500)

// Config 网关配置
type Config struct {
	ServerURL       string
	ClientVersion   string
	Platform        string
	OS              string
	HeartbeatMillis int64
}

// Client 网关客户端
type Client struct {
	cfg          Config
	conn         *websocket.Conn
	authToken    string
	firstToken   string // 首次请求(登录)的 ACE 初始化凭据
	seq          int64
	mu           sync.Mutex
	writeMu      sync.Mutex // 序列化 WebSocket 写：避免并发 goroutine（自动化 + 前端 HTTP handler + 心跳）同写一条连接导致帧交错损坏（nhooyr.io/websocket 不支持并发写）
	pending      map[int64]chan *proto.Message
	// 请求槽位机制（对齐参考实现）：限制单连接并发在途请求数，避免 ACE 轮询占满网关
	// 单槽位后游戏请求(AllLands/Bag)被饿死 → 后台“在线但无数据/卡死”。
	normalSlots chan struct{} // 普通请求(非心跳)并发上限 = 8
	totalSlots  chan struct{} // 全部请求(含心跳/ACE)并发上限 = 10
	queued      atomic.Int64
	active      atomic.Int64
	kickHook          func() // 被踢（账号在别处登录等致命码）时由连接池注入：关闭连接并触发应用宝离线重连（对齐 Node kickout→reconnect）
	timeoutCloseHook  func() // 超时断连（服务端抖动/瞬时限流）时由连接池注入：标记该断连为“超时型”，重连不计上限
	disconnectHook    func(reason string) // 连接异常断开（读错误/心跳连续失败）时由连接池注入：写前端可见的掉线日志
	accountID    string
	giftHook     func(accountID string, delta int64)
	// lastBagSync 最近一次从背包同步点券/金豆的时间（节流用，避免首页高并发拉 Bag 触发风控）
	lastBagSync  time.Time
	farmPushHook func(accountID string)
	GID          int64
	landsBytes   []byte // 预拉缓存：AllLands 原始 body
	landsAt      time.Time
	careerBytes  []byte // 生涯统计缓存：CareerInfoGet 原始 body（数据稳定，TTL 命中直接读，减少请求/竞争）
	careerAt     time.Time
	userName     string
	level        int64
	gold         int64
	exp          int64
	coupon       int64
	goldBean     int64
	avatar       string // 玩家头像 URL（登录后或首次获取生涯统计时缓存）
	openID       string // 登录用户 openId（ACE 反作弊身份绑定，对齐 Node bindUser）

	ace *ace.Runtime

	closed    chan struct{} // 断开通知通道：幂等关闭
	closeOnce sync.Once

	ctx    context.Context
	cancel context.CancelFunc
}

// New 创建客户端
func New(cfg Config) *Client {
	if cfg.HeartbeatMillis <= 0 {
		cfg.HeartbeatMillis = 25000
	}
	return &Client{
		cfg:         cfg,
		pending:     map[int64]chan *proto.Message{},
		closed:      make(chan struct{}),
		normalSlots: make(chan struct{}, 8),
		totalSlots:  make(chan struct{}, 10),
	}
}

// gatewayToken 生成认证 token（官方随机 base62 + "="）
func gatewayToken() string {
	const alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	length := 64 + randInt(64) // 64~127
	b := make([]byte, length)
	for i := range b {
		b[i] = alpha[randInt(len(alpha))]
	}
	return string(b) + "="
}

func randInt(n int) int {
	if n <= 0 {
		return 0
	}
	buf := make([]byte, 1)
	rand.Read(buf)
	return int(buf[0]) % n
}

// Connect 建立连接并登录
func (c *Client) Connect(ctx context.Context, code string) error {
	// 初始化 ACE 安全运行时
	c.ace = ace.New("gw", 3167, "0")
	if err := c.ace.Init(ctx); err != nil {
		return fmt.Errorf("ace init: %w", err)
	}
	// 注意：ACE 初始化凭据(firstToken)不在此预生成。对齐 Node network.js：登录请求用
	// 随机 gatewayToken（initialGamePackInfo 此时为空），bindUser(openId) 之后才
	// getEncryptedInitInfo 生成“带用户”令牌供后续请求使用。故登录请求将走 c.authToken
	// （随机），与 Node createGatewayToken() 行为逐字节一致；绑定后的 firstToken 仅用于登录后首请求(Prime)。

	url := fmt.Sprintf("%s?platform=%s&os=%s&ver=%s&code=%s&openID=",
		c.cfg.ServerURL, c.cfg.Platform, c.cfg.OS, c.cfg.ClientVersion, code)

	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"User-Agent": []string{defaultUserAgent},
			"Origin":     []string{"https://gate-obt.nqf.qq.com"},
		},
	})
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	c.conn = conn
	c.conn.SetReadLimit(100 * 1024 * 1024) // align with Node ref (ws default maxPayload=100MiB); nhooyr default 32KB chokes on large Bag/AllLands frames -> StatusMessageTooBig -> reconnect storm
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.authToken = gatewayToken()

	// 读循环
	go c.readLoop()

	// 登录
	if err := c.login(ctx, code); err != nil {
		return err
	}
	// ACE 反作弊身份绑定：登录拿到 openId 后必须先 BindUser 再重新生成初始化凭据，
	// 否则后续请求携带的 ACE 令牌为"无用户"状态，反作弊上报身份残缺 → 风控封号。
	// 对齐 Node network.js: bindUser(openId) → getEncryptedInitInfo() → startHeartbeat/startAce。
	if c.openID != "" {
		if berr := c.ace.BindUser(c.openID); berr != nil {
			log.Printf("[gw] 账号 %s ACE BindUser 失败: %v", c.openID, berr)
		} else if initInfo, ierr := c.ace.EncryptedInitInfo(); ierr == nil && initInfo != "" {
			c.firstToken = initInfo // 覆盖登录前生成的未绑定令牌，供后续请求使用
		}
	}
	return nil
}

// login 发送登录请求并等待回复
func (c *Client) login(ctx context.Context, code string) error {
	body := proto.EncodeLoginRequest(c.cfg.ClientVersion)
	rep, err := c.Request(ctx, "gamepb.userpb.UserService", "Login", body, 20*time.Second)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	lr := proto.DecodeLoginReply(rep.Body)
	if lr.Basic == nil {
		return fmt.Errorf("login: empty basic")
	}
	c.GID = lr.Basic.GID
	c.userName = lr.Basic.Name
	c.level = lr.Basic.Level
	c.gold = lr.Basic.Gold
	c.exp = lr.Basic.Exp
	if lr.Basic.Avatar != "" {
		c.avatar = lr.Basic.Avatar
	}
	c.openID = lr.Basic.OpenID // 缓存 openId 供 ACE BindUser 使用
	return nil
}

// Request 发送请求并等待响应（按 client_seq 匹配）
func (c *Client) Request(ctx context.Context, service, method string, body []byte, timeout time.Duration) (*proto.Message, error) {
	// 超时覆盖整个请求生命周期（含等待槽位）：单连接并发在途请求可能占满槽位，若此时用
	// 无 deadline 的 ctx(前端 r.Context()/自动化 context.Background()) acquire 会无限等待，
	// 表现为“点页面一直转圈/假卡死”。统一先加超时，槽位满时最多等 timeout 即快速失败。
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 槽位机制：限制单连接并发在途请求数，避免 ACE 轮询占满网关单槽位后
	// 游戏请求(AllLands/Bag)被饿死 → 后台“在线但无数据/卡死”。对齐参考实现。
	release, err := c.acquire(ctx2, method == "Heartbeat")
	if err != nil {
		return nil, err
	}
	defer release()

	c.mu.Lock()
	c.seq++
	seq := c.seq
	ch := make(chan *proto.Message, 1)
	c.pending[seq] = ch
	c.mu.Unlock()

	if c.conn == nil {
		c.removePending(seq)
		return nil, fmt.Errorf("gateway connection is not open")
	}

	meta := &proto.Meta{
		ServiceName: service,
		MethodName:  method,
		MessageType: proto.MsgTypeRequest,
		ClientSeq:   seq,
	}
	// body 用 ACE 加密
	encBody := body
	if len(body) > 0 {
		eb, eerr := c.ace.Encrypt(body)
		if eerr != nil {
			c.removePending(seq)
			return nil, fmt.Errorf("ace encrypt: %w", eerr)
		}
		encBody = eb
	}
	// auth_token: 首次(登录)用 ACE 初始化凭据，之后用随机 token。
	// firstToken 读写加 mu 锁，修复并发首批请求重复消费首令牌的竞态（对齐参考实现 takeToken 用 connMu 保护）。
	c.mu.Lock()
	token := c.authToken
	if c.firstToken != "" {
		token = c.firstToken
		c.firstToken = ""
	}
	c.mu.Unlock()
	payload := proto.EncodeMessage(meta, encBody, token)

	// 串行化写：nhooyr.io/websocket 不允许并发 Write，而 Go 多线程下单连接会被
	// 自动化 goroutine / 前端 handler / 心跳同时写，不加锁会交错损坏帧 → 网关超时/拿不到数据。
	// 仅 Write 阶段持锁，readLoop 在另一把锁(mu)上读 pending，不会死锁。
	c.writeMu.Lock()
	writeErr := c.conn.Write(ctx2, websocket.MessageBinary, payload)
	c.writeMu.Unlock()
	if writeErr != nil {
		c.removePending(seq)
		return nil, writeErr
	}

	select {
	case msg := <-ch:
		if msg.Meta != nil && msg.Meta.ErrorCode != 0 {
			// 账号在别处登录等致命码：触发连接池重连（对齐 Node kickout 事件），并关闭当前连接。
			if isKickCode(msg.Meta.ErrorCode) && c.kickHook != nil {
				log.Printf("[gw] 账号被踢下线 code=%d %s，触发应用宝重连", msg.Meta.ErrorCode, msg.Meta.ErrorMessage)
				go c.kickHook()
				c.Close()
			}
			return msg, &gwError{Code: msg.Meta.ErrorCode, Message: msg.Meta.ErrorMessage}
		}
		return msg, nil
	case <-ctx2.Done():
		c.removePending(seq)
		// 非 ACE/非心跳的请求超时 → 关闭连接触发重连（对齐参考实现）：
		// 死连接不应长期留在缓存里伪装“在线”骗前端，应被真正断开并重建。
		if errors.Is(ctx2.Err(), context.DeadlineExceeded) && shouldCloseConnectionAfterTimeout(service, method) {
			log.Printf("[gw] 账号 %s 请求 %s.%s 超时，关闭连接触发重连", c.accountID, service, method)
			if c.timeoutCloseHook != nil {
				c.timeoutCloseHook()
			}
			c.closeActiveConnection()
		}
		return nil, fmt.Errorf("request timeout: %s.%s", service, method)
	}
}

func (c *Client) removePending(seq int64) {
	c.mu.Lock()
	delete(c.pending, seq)
	c.mu.Unlock()
}

// readLoop 持续读取消息
func (c *Client) readLoop() {
	for {
		_, data, err := c.conn.Read(c.ctx)
		if err != nil {
			// 仅异常断开（非主动 Close）才报掉线日志：主动 Close 会先 cancel ctx，
			// 此时 ctx.Err() != nil → 不重复打扰（被踢/服务停止/手动断开都属于主动侧）
			if c.ctx.Err() == nil && c.disconnectHook != nil {
				c.disconnectHook("连接读错误: " + err.Error())
			}
			c.Close()
			return
		}
		msg := proto.DecodeMessage(data)
		if msg.Meta == nil {
			continue
		}
		// 响应 body 为明文（Node 不加密响应，直接 decode），不做 ACE 解密
		if msg.Meta.MessageType == proto.MsgTypeResponse {
			c.mu.Lock()
			ch := c.pending[msg.Meta.ClientSeq]
			c.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
		}
		// Notify 推送：对齐权威 Node network.ts handleNotify——外层先解 EventMessage，
		// 类型标识在 message_type、真正的业务通知体在 body（不能再用 Meta.MethodName 判断，
		// 也不能直接拿 EventMessage 当 ItemNotify 解码）。
		if msg.Meta.MessageType == proto.MsgTypeNotify {
			typ, eventBody, okE := proto.DecodeEventMessage(msg.Body)
			if !okE {
				continue
			}
			// ItemNotify 物品变化 → 同气连枝礼包(101351)增量 + 经验/金币/点券/金豆豆
			if strings.Contains(typ, "ItemNotify") {
				c.applyItemNotify(eventBody)
			}
			// BasicNotify 基础信息变化 → 用服务端绝对余额/经验/等级校准 c.gold/c.exp/c.level
			// （对齐 Node network.ts BasicNotify：notify.basic.gold→userState.gold），
			// 避免 ItemNotify 把变化量当余额覆盖后、真实余额无法恢复（首页金币错值/autoSell 差值爆数）。
			if strings.Contains(typ, "BasicNotify") {
				c.applyBasicNotify(eventBody)
			}
			// LandsNotify 土地变化（被放虫/放草/偷菜等）→ 触发 farm_push
			if strings.Contains(typ, "LandsNotify") {
				host := proto.DecodeLandsNotifyHostGid(eventBody)
				if c.farmPushHook != nil && (host == 0 || host == c.GID) {
					c.farmPushHook(c.accountID)
				}
			}
		}
	}
}

// applyItemNotify 处理物品变化推送：增量更新内存 + 同气礼包累计（对齐 Node network.js）
func (c *Client) applyItemNotify(body []byte) {
	items := proto.DecodeItemNotify(body)
	if len(items) == 0 {
		return
	}
	c.mu.Lock()
	for _, it := range items {
		switch it.ID {
		case 1101: // 经验
			if it.Count > 0 {
				c.exp = it.Count
			} else if it.Delta != 0 {
				c.exp = max64(0, c.exp+it.Delta)
			}
		case 1, 1001: // 金币
			if it.Count > 0 {
				c.gold = it.Count
			} else if it.Delta != 0 {
				c.gold = max64(0, c.gold+it.Delta)
			}
		case 1002: // 点券
			if it.Count > 0 {
				c.coupon = it.Count
			} else if it.Delta != 0 {
				c.coupon = max64(0, c.coupon+it.Delta)
			}
		case 1005: // 金豆豆
			if it.Count > 0 {
				c.goldBean = it.Count
			} else if it.Delta != 0 {
				c.goldBean = max64(0, c.goldBean+it.Delta)
			}
		case proto.ItemIDTongQiGift: // 同气连枝礼包
			if c.giftHook != nil {
				delta := it.Delta
				if delta <= 0 && it.Count > 0 {
					delta = 1
				}
				if delta > 0 {
					c.giftHook(c.accountID, delta)
				}
			}
		}
	}
	c.mu.Unlock()
}

// applyBasicNotify 基础信息变化通知：用服务端绝对余额/经验/等级校准内存缓存。
// 对齐权威 Node network.ts handleNotify BasicNotify：notify.basic.gold → userState.gold 等，
// 避免 ItemNotify 把变化量当余额覆盖后、真实余额无法恢复（首页金币错值 / autoSell 差值爆数）。
func (c *Client) applyBasicNotify(body []byte) {
	bi := proto.DecodeBasicNotify(body)
	if bi == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if bi.Gold > 0 {
		c.gold = bi.Gold
	}
	if bi.Exp > 0 {
		c.exp = bi.Exp
	}
	if bi.Level > 0 {
		c.level = bi.Level
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// SetGiftHook 注入同气礼包回调（由连接池在创建时设置）
func (c *Client) SetGiftHook(accountID string, hook func(accountID string, delta int64)) {
	c.accountID = accountID
	c.giftHook = hook
}

// SetFarmPushHook 注入农场推送回调（推送触发巡田；由连接池在创建时注册）
func (c *Client) SetFarmPushHook(hook func(accountID string)) {
	c.farmPushHook = hook
}

// SetKickHook 设置被踢回调（连接池在创建连接时注册，用于触发自动重连）
func (c *Client) SetKickHook(f func()) {
	c.kickHook = f
}

// SetTimeoutCloseHook 设置超时断连回调（连接池在创建连接时注册，用于标记此类断连为“超时型”）
func (c *Client) SetTimeoutCloseHook(f func()) {
	c.timeoutCloseHook = f
}

// SetDisconnectHook 设置连接异常断开回调（连接池在创建连接时注册，用于写前端可见的掉线日志）
func (c *Client) SetDisconnectHook(f func(reason string)) {
	c.disconnectHook = f
}

// isKickCode 是否为需要重连的致命网关错误码（如 1000014=账号已在其他地方登录）。
// 仅识别已确认的踢下线码，避免把瞬时错误误判为被踢而频繁重连。
func isKickCode(code int64) bool {
	return code == 1000014
}

// gwError 携带网关错误码的结构化错误（对标 Node 网关返回的 ErrorCode/ErrorMessage），
// 供连接池在不依赖字符串匹配的前提下判断封号等致命错误。
type gwError struct {
	Code    int64
	Message string
}

func (e *gwError) Error() string {
	return fmt.Sprintf("code=%d %s", e.Code, e.Message)
}

// BanCode 封号错误码：权限不足，不能登录（账号被封禁，任何重连都是无效且可能加重风险）。
const BanCode int64 = 1000016

// IsBanError 判断错误是否为封号错误（权限不足，不能登录 code=1000016）。
// 通过 errors.As 解开 fmt.Errorf("%w") 包装链，可靠匹配而非字符串硬匹配。
func IsBanError(err error) bool {
	var ge *gwError
	if errors.As(err, &ge) {
		return ge.Code == BanCode
	}
	return false
}

// prime 登录成功后预拉首页所需数据缓存（对齐 Node 常驻预载）
func (c *Client) Prime() {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if rep, err := c.Request(ctx, "gamepb.plantpb.PlantService", "AllLands",
		proto.EncodeAllLandsRequest(0), 10*time.Second); err == nil {
		c.landsBytes = rep.Body
		c.landsAt = time.Now()
	}
	// 券/豆未填充时补拉
	if c.coupon == 0 && c.goldBean == 0 {
		c.FetchBagAssets(ctx)
	}
}

// StoreLands 写入/刷新农场缓存（操作成功后强制重拉时更新）
func (c *Client) StoreLands(body []byte) {
	c.landsBytes = body
	c.landsAt = time.Now()
}

// LandsCached 读取缓存的农场数据（ttl 内命中）
func (c *Client) LandsCached(ttl time.Duration) ([]byte, bool) {
	if c.landsBytes != nil && time.Since(c.landsAt) < ttl {
		return c.landsBytes, true
	}
	return nil, false
}

// StoreCareer 写入/刷新生涯统计缓存
func (c *Client) StoreCareer(body []byte) {
	c.careerBytes = body
	c.careerAt = time.Now()
}

// CareerCached 读取缓存的生涯统计数据（ttl 内命中；生涯数据稳定，TTL 内不重复请求）
func (c *Client) CareerCached(ttl time.Duration) ([]byte, bool) {
	if c.careerBytes != nil && time.Since(c.careerAt) < ttl {
		return c.careerBytes, true
	}
	return nil, false
}

// StartHeartbeat 启动心跳。对齐 Node：心跳连续 miss 达阈值（约 5 次无响应）判定断线，
// 触发 Close()（power-close the closed channel），交由自动重连接管；容忍瞬时抖动。
func (c *Client) StartHeartbeat(ctx context.Context) {
	iv := time.Duration(c.cfg.HeartbeatMillis) * time.Millisecond
	missLimit := 5 // 连续丢心跳次数阈值（~5*25s≈125s 无响应判断线）
	go func() {
		t := time.NewTicker(iv)
		defer t.Stop()
		miss := 0
		for {
			select {
			case <-t.C:
				if c.IsClosed() {
					return
				}
				body := proto.EncodeHeartbeatRequest(c.GID, c.cfg.ClientVersion)
				ct, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				_, hbErr := c.Request(ct, "gamepb.userpb.UserService", "Heartbeat", body, 15*time.Second)
				cancel()
				if hbErr != nil {
					miss++
					if miss >= missLimit {
						if c.disconnectHook != nil {
							c.disconnectHook("心跳连续失败（" + hbErr.Error() + "），判定死连接")
						}
						c.Close() // 触发断开通知；幂等，readLoop 亦会调用
						return
					}
				} else {
					miss = 0 // 收到响应：瞬时抖动消解
				}
			case <-c.ctx.Done():
				return
			}
		}
	}()
}

// HeartbeatOnce 发送一次游戏网络心跳，交由账号串行执行线（automationLoop）驱动。
// 对齐 Node network.startHeartbeat：不再存在独立心跳 goroutine，消除对 TSDK 的并发访问。
func (c *Client) HeartbeatOnce() bool {
	if c.IsClosed() {
		return false
	}
	body := proto.EncodeHeartbeatRequest(c.GID, c.cfg.ClientVersion)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := c.Request(ctx, "gamepb.userpb.UserService", "Heartbeat", body, 15*time.Second)
	return err == nil
}

// SetClientVersion 更新客户端版本号（热更新：保存系统配置后秒级生效，无需重连；对齐 Node config_sync）
func (c *Client) SetClientVersion(v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg.ClientVersion = v
}

// IsClosed 连接是否已断开（幂等，非阻塞）
func (c *Client) IsClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// Done 返回连接断开通知通道：断开时（readLoop 读到错误或主动 Close）幂等关闭
func (c *Client) Done() <-chan struct{} {
	return c.closed
}

// ---- ACE TSDK 运行时透传（供 AceService 调度调用，对齐 Node tsdk-runtime.js） ----

// ACEProcessReceivedData 处理下行数据队列
func (c *Client) ACEProcessReceivedData() error {
	if c.ace == nil {
		return fmt.Errorf("ace runtime not initialized")
	}
	return c.ace.ProcessReceivedData()
}

// ACEHeartbeatTick TSDK 心跳
func (c *Client) ACEHeartbeatTick() error {
	if c.ace == nil {
		return fmt.Errorf("ace runtime not initialized")
	}
	return c.ace.HeartbeatTick()
}

// ACEDetectSpeedHack 速度检测
func (c *Client) ACEDetectSpeedHack(elapsedMs int64) error {
	if c.ace == nil {
		return fmt.Errorf("ace runtime not initialized")
	}
	return c.ace.DetectSpeedHack(elapsedMs)
}

// ACESendStatus 状态上报
func (c *Client) ACESendStatus() error {
	if c.ace == nil {
		return fmt.Errorf("ace runtime not initialized")
	}
	return c.ace.SendStatus()
}

// ACECheckFunctionArray 完整性校验
func (c *Client) ACECheckFunctionArray(names []string, typeFlag int64) error {
	if c.ace == nil {
		return fmt.Errorf("ace runtime not initialized")
	}
	return c.ace.CheckFunctionArray(names, typeFlag)
}

// ACEGetDataToServer 取待上报数据
func (c *Client) ACEGetDataToServer() ([]byte, error) {
	if c.ace == nil {
		return nil, fmt.Errorf("ace runtime not initialized")
	}
	return c.ace.GetDataToServer()
}

// ACESendDataFromServer 回灌服务端下发数据
func (c *Client) ACESendDataFromServer(data []byte) error {
	if c.ace == nil {
		return fmt.Errorf("ace runtime not initialized")
	}
	return c.ace.SendDataFromServer(data)
}

// Close 关闭连接
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		if c.conn != nil {
			c.conn.Close(websocket.StatusNormalClosure, "")
		}
		// 连接关闭时让所有在途请求立即失败，避免前端/自动化各自苦等满 15s 超时（对齐参考实现 failPending）
		c.failPending(errors.New("gateway connection closed"))
		close(c.closed)
	})
}

// failPending 连接断开时立即让所有在途请求失败（发送哨兵错误帧），
// 避免每条请求各自等待其 15s 超时 → 前端/后台不再整体卡死。
func (c *Client) failPending(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[int64]chan *proto.Message)
	c.mu.Unlock()
	for _, ch := range pending {
		select {
		case ch <- &proto.Message{Meta: &proto.Meta{ErrorCode: -1, ErrorMessage: err.Error()}}:
		default:
		}
	}
}

// acquire 获取请求槽位：普通请求占用 normalSlots(上限8)+totalSlots(上限10)，
// 心跳仅占用 totalSlots(可绕过 normalSlots，保证存活探测不被游戏请求挤占)。
// 上下文取消时立即返回错误，避免调用方无限等待槽位。
func (c *Client) acquire(ctx context.Context, heartbeat bool) (func(), error) {
	queued := c.queued.Add(1)
	if queued > maxQueued {
		c.queued.Add(-1)
		return nil, errors.New("gateway request queue is full")
	}
	defer c.queued.Add(-1)
	if !heartbeat {
		select {
		case c.normalSlots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	select {
	case c.totalSlots <- struct{}{}:
		c.active.Add(1)
		return func() {
			<-c.totalSlots
			if !heartbeat {
				<-c.normalSlots
			}
			c.active.Add(-1)
		}, nil
	case <-ctx.Done():
		if !heartbeat {
			<-c.normalSlots
		}
		return nil, ctx.Err()
	}
}

// shouldCloseConnectionAfterTimeout 判断某请求超时后是否应关闭整条连接触发重连。
// 心跳与 ACE 上报失败不应关连接（心跳失败由心跳循环独立判定；ACE 上报失败不应反噬游戏请求通道），
// 其余游戏请求超时则关连接重建，避免死连接长期伪装“在线”。对齐参考实现。
func shouldCloseConnectionAfterTimeout(service, method string) bool {
	if method == "Heartbeat" {
		return false
	}
	// 离开好友农场是收尾/清理请求,超时直接忽略,不反噬整条连接触发重连。
	// 对齐 Node friend-api.js:768 `// Ignore leave errors`。
	if method == "Leave" {
		return false
	}
	return service != "gamepb.acepb.AceService"
}

// closeActiveConnection 立即关闭底层 WebSocket 连接，置 IsClosed 并 failPending，
// 使连接池下次 Get() 走重连流程（对齐参考实现 closeActiveConnection + disconnect）。
func (c *Client) closeActiveConnection() {
	c.Close()
}

// UserName 登录用户名
func (c *Client) UserName() string { return c.userName }

// Level 等级
func (c *Client) Level() int64 { return c.level }

// Gold 金币
func (c *Client) Gold() int64 { return c.gold }

// Exp 经验
func (c *Client) Exp() int64 { return c.exp }

// Avatar 玩家头像 URL（可能为空，需先获取生涯统计或登录后缓存）
func (c *Client) Avatar() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.avatar
}

// SetAvatar 缓存玩家头像 URL（首次获取生涯统计时调用）
func (c *Client) SetAvatar(url string) {
	c.mu.Lock()
	c.avatar = url
	c.mu.Unlock()
}

// Coupon 点券余额
func (c *Client) Coupon() int64 { return c.coupon }

// GoldBean 金豆余额
func (c *Client) GoldBean() int64 { return c.goldBean }

// FetchBagAssets 从背包拉取点券(1002)/金豆(1005)余额
func (c *Client) FetchBagAssets(ctx context.Context) error {
	rep, err := c.Request(ctx, "gamepb.itempb.ItemService", "Bag",
		proto.EncodeBagRequest(), 10*time.Second)
	if err != nil {
		return err
	}
	c.ApplyBagAssets(proto.DecodeBagReply(rep.Body))
	return nil
}

// ApplyBagAssets 从背包物品同步点券/金豆绝对持有量并记录同步时间（并发安全）。
// 对齐权威 worker.ts：登录后拉背包按物品 ID 1002/1005 的 count 覆盖。
func (c *Client) ApplyBagAssets(br *proto.BagReply) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, it := range br.Items {
		if it.ID == proto.ItemIDCoupon {
			c.coupon = it.Count
		} else if it.ID == proto.ItemIDGoldBean {
			c.goldBean = it.Count
		}
	}
	c.lastBagSync = time.Now()
}

// EnsureBagAssets 节流式同步点券/金豆：距上次同步<15s 复用内存值，否则拉一次背包校准。
// 供首页 /api/home/profile 等低频展示接口调用，避免 ItemNotify 变化量污染后无法恢复。
func (c *Client) EnsureBagAssets(ctx context.Context) error {
	c.mu.Lock()
	last := c.lastBagSync
	c.mu.Unlock()
	if time.Since(last) < 15*time.Second {
		return nil
	}
	rep, err := c.Request(ctx, "gamepb.itempb.ItemService", "Bag",
		proto.EncodeBagRequest(), 10*time.Second)
	if err != nil {
		return err
	}
	c.ApplyBagAssets(proto.DecodeBagReply(rep.Body))
	return nil
}

// ReportArkClick 主动加好友：分享卡数据 → UserService.ReportArkClick
func (c *Client) ReportArkClick(ctx context.Context, uid int64, openId, shareKey string) error {
	_, err := c.Request(ctx, "gamepb.userpb.UserService", "ReportArkClick",
		proto.EncodeReportArkClickRequest(uid, openId, shareKey), 10*time.Second)
	return err
}
