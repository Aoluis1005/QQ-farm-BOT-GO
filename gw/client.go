// Package gw 腾讯农场网关 WebSocket 客户端
package gw

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/Aoluis1005/go-farm-bot/ace"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// Node 默认微信 UA
// iOS Safari UA（wx+iOS 唯一可握手的 UA）
const defaultUserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 26_2_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

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
	kickHook     func() // 被踢（账号在别处登录等致命码）时由连接池注入：关闭连接并触发应用宝离线重连（对齐 Node kickout→reconnect）
	accountID    string
	giftHook     func(accountID string, delta int64)
	farmPushHook func(accountID string)
	GID          int64
	landsBytes   []byte // 预拉缓存：AllLands 原始 body
	landsAt      time.Time
	userName     string
	level        int64
	gold         int64
	exp          int64
	coupon       int64
	goldBean     int64
	avatar       string // 玩家头像 URL（登录后或首次获取生涯统计时缓存）

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
		cfg:     cfg,
		pending: map[int64]chan *proto.Message{},
		closed:  make(chan struct{}),
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
	initInfo, err := c.ace.EncryptedInitInfo()
	if err != nil || initInfo == "" {
		return fmt.Errorf("ace initInfo: %w", err)
	}
	c.firstToken = initInfo

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
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.authToken = gatewayToken()

	// 读循环
	go c.readLoop()

	// 登录
	if err := c.login(ctx, code); err != nil {
		return err
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
	return nil
}

// Request 发送请求并等待响应（按 client_seq 匹配）
func (c *Client) Request(ctx context.Context, service, method string, body []byte, timeout time.Duration) (*proto.Message, error) {
	c.mu.Lock()
	c.seq++
	seq := c.seq
	ch := make(chan *proto.Message, 1)
	c.pending[seq] = ch
	c.mu.Unlock()

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
	// auth_token: 首次(登录)用 ACE 初始化凭据，之后用随机 token
	token := c.authToken
	if c.firstToken != "" {
		token = c.firstToken
		c.firstToken = ""
	}
	payload := proto.EncodeMessage(meta, encBody, token)

	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

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
			return msg, fmt.Errorf("%s.%s code=%d %s", service, method, msg.Meta.ErrorCode, msg.Meta.ErrorMessage)
		}
		return msg, nil
	case <-ctx2.Done():
		c.removePending(seq)
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
		// Notify 推送：ItemNotify 物品变化 → 检测同气连枝礼包(101351)增量
		if msg.Meta.MessageType == proto.MsgTypeNotify &&
			strings.Contains(msg.Meta.MethodName, "ItemNotify") {
			c.applyItemNotify(msg.Body)
		}
		// Notify 推送：LandsNotify 土地变化（被放虫/放草/偷菜等）→ 触发 farm_push（对齐 Node landsChanged→onLandsChangedPush）
		if msg.Meta.MessageType == proto.MsgTypeNotify &&
			strings.Contains(msg.Meta.MethodName, "LandsNotify") {
			host := proto.DecodeLandsNotifyHostGid(msg.Body)
			if c.farmPushHook != nil && (host == 0 || host == c.GID) {
				c.farmPushHook(c.accountID)
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

// isKickCode 是否为需要重连的致命网关错误码（如 1000014=账号已在其他地方登录）。
// 仅识别已确认的踢下线码，避免把瞬时错误误判为被踢而频繁重连。
func isKickCode(code int64) bool {
	return code == 1000014
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
		close(c.closed)
	})
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
	br := proto.DecodeBagReply(rep.Body)
	for _, it := range br.Items {
		if it.ID == proto.ItemIDCoupon {
			c.coupon = it.Count
		} else if it.ID == proto.ItemIDGoldBean {
			c.goldBean = it.Count
		}
	}
	return nil
}

// ReportArkClick 主动加好友：分享卡数据 → UserService.ReportArkClick
func (c *Client) ReportArkClick(ctx context.Context, uid int64, openId, shareKey string) error {
	_, err := c.Request(ctx, "gamepb.userpb.UserService", "ReportArkClick",
		proto.EncodeReportArkClickRequest(uid, openId, shareKey), 10*time.Second)
	return err
}
