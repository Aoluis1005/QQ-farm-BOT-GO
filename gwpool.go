package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/models"
)

// ClientPool 网关连接池：按账号缓存已登录的网关连接
type ClientPool struct {
	mu sync.Mutex
	m  map[string]*gw.Client // accountID -> 已登录连接

	// 单飞连接：同一账号同时只进行一个 connect（前端并发 Get / onKick / scanAutoReconnect
	// 共享这一次登录结果），杜绝多连接并发登录触发游戏“账号已在其他地方登录”自踢死循环。
	inflight map[string]chan connectResult

	// 掉线自动重连状态（accountID -> 状态）
	offlineSince     map[string]time.Time // 首次检测到断线的时间
	reconnectAttempts map[string]int      // 重连计数（对齐 Node：成功不清零，仅手动停止/踢下线/删除账号时清零）
	stopped           map[string]bool     // 达上限后停止自动重连，直到手动触发/重新连上
	kickBackoffUntil  map[string]time.Time // 被踢后重连防抖：下次允许重连的最早时间（避免与别处登录互踢自旋）
}

// connectResult 单飞连接的返回（连接或错误）
type connectResult struct {
	c   *gw.Client
	err error
}

var clientPool = &ClientPool{
	m:                 map[string]*gw.Client{},
	inflight:          map[string]chan connectResult{},
	offlineSince:      map[string]time.Time{},
	reconnectAttempts: map[string]int{},
	stopped:           map[string]bool{},
	kickBackoffUntil:  map[string]time.Time{},
}

func gwConfig(platform string) gw.Config {
	if platform == "" {
		platform = "qq"
	} else if platform != "qq" && platform != "wx" {
		// YYB 扫码等渠道本质是 WX 渠道（对标 Node platform='wx'）
		platform = "wx"
	}
	// 客户端版本号从系统配置读取（对齐 Node CONFIG.clientVersion），空则回退默认
	cv := models.GetSystemConfig().ClientVersion
	if cv == "" {
		cv = "1.13.0.4_20260723"
	}
	return gw.Config{
		ServerURL:       "wss://gate-obt.nqf.qq.com/prod/ws",
		ClientVersion:   cv,
		Platform:        platform,
		OS:              "iOS",
		HeartbeatMillis: 25000,
	}
}

func (p *ClientPool) cached(accountID string) *gw.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.m[accountID]; ok && c != nil && c.GID != 0 {
		return c
	}
	return nil
}

func (p *ClientPool) store(accountID string, c *gw.Client) {
	c.SetKickHook(func() { p.onKick(accountID) })
	p.mu.Lock()
	if old, ok := p.m[accountID]; ok && old != nil && old != c && !old.IsClosed() {
		// 替换旧连接时关闭它，避免泄漏（被踢的旧连接通常已关闭，此处幂等）
		old.Close()
	}
	p.m[accountID] = c
	p.mu.Unlock()
}

// onKick 被踢下线后的自动重连（对齐 Node kickout → 应用宝离线重连）：
// 优先用 YYB openid 刷新 code 再连；带防抖避免与“别处登录”互踢形成自旋。
// 仅当距上次重连超过冷却窗才执行，否则跳过（交给用户自行解决冲突端）。
func (p *ClientPool) onKick(accountID string) {
	p.mu.Lock()
	if until, ok := p.kickBackoffUntil[accountID]; ok && time.Now().Before(until) {
		p.mu.Unlock()
		return
	}
	p.kickBackoffUntil[accountID] = time.Now().Add(8 * time.Second) // 冷却 8s，防止互踢自旋
	p.reconnectAttempts[accountID]++
	p.mu.Unlock()

	acc := models.GetAccountByID(accountID)
	if acc == nil {
		return
	}
	// 优先 YYB 刷新 code（账号须有 openid）；刷新失败则用旧 code 尝试
	if newCode, cerr := refreshCodeFromYyb(acc); cerr == nil && newCode != "" {
		acc.Code = newCode
		models.AddOrUpdateAccount(*acc)
	}
	// 单飞连接：若此刻已有其他路径在连同一账号，复用其结果，避免自踢
	if _, err := p.connectLocked(acc); err != nil {
		log.Printf("[pool] 账号 %s 被踢后重连失败: %v", accountID, err)
		return
	}
	log.Printf("[pool] 账号 %s 被踢后已重连（第 %d 次）", accountID, p.reconnectAttempts[accountID])
}

// connect 用账号 code 连接并登录
func connect(acc *models.Account) (*gw.Client, error) {
	c := gw.New(gwConfig(acc.Platform))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Connect(ctx, acc.Code); err != nil {
		return nil, fmt.Errorf("连接网关失败: %w", err)
	}
	c.SetGiftHook(acc.ID, recordGift)
	c.Prime() // 登录后立即预拉首页数据缓存
	c.StartHeartbeat(context.Background())
	// 对齐 Node network.js:583-584：登录成功后 startHeartbeat() + startAceService()
	// ACE 上报服务随连接关闭自动停止（监听 Done()）
	aceSvc := startAceService(c, acc.ID)
	go func() {
		<-c.Done()
		aceSvc.stop()
	}()
	appendOpLog(acc.ID, "登录", "账号上线")
	return c, nil
}

// refreshCodeFromYyb 用账号 openid 换新 code，成功则更新账号并返回。
// 优先走内置 YYB（embeddedYybBaseURL + embed.GetApiToken()），未配置内置才回退 YYB_API_URL/YYB_API_KEY，
// 与 account_api.go resolveYybCreds 完全一致；账号无 openid 时报错。
func refreshCodeFromYyb(acc *models.Account) (string, error) {
	if acc.OpenID == "" {
		return "", fmt.Errorf("账号无 openid，无法自动刷新")
	}
	apiBase, apiKey := resolveYybCreds(nil)
	return getCodeFromYyb(apiBase, apiKey, acc.OpenID, "")
}

// connectLocked 单飞连接：同一账号同时只有一个 connect 在进行（对齐 Node 单线程登录语义）。
// 并发调用方（前端多请求 Get / onKick / scanAutoReconnect / 手动重试）共享同一次登录结果，
// 避免多连接并发登录触发游戏“账号已在其他地方登录”自踢死循环。
// 连接失败且 code 疑似过期时，用 openid 自动刷新一次后重试（对齐 Node refreshYybCodeIfNeeded）。
func (p *ClientPool) connectLocked(acc *models.Account) (*gw.Client, error) {
	p.mu.Lock()
	if ch, ok := p.inflight[acc.ID]; ok {
		// 已有连接在进行中，挂等其结果（不新建第二个连接）
		p.mu.Unlock()
		res := <-ch
		return res.c, res.err
	}
	ch := make(chan connectResult, 1)
	p.inflight[acc.ID] = ch
	p.mu.Unlock()

	c, err := connect(acc)
	if err != nil {
		// code 过期 → 用持久 openid 自动刷新重试一次
		if newCode, cerr := refreshCodeFromYyb(acc); cerr == nil && newCode != "" {
			acc.Code = newCode
			models.AddOrUpdateAccount(*acc)
			c, err = connect(acc)
		}
	}
	if err == nil {
		loadAssetsAsync(acc.ID, c)
		p.store(acc.ID, c)
	}

	p.mu.Lock()
	delete(p.inflight, acc.ID)
	p.mu.Unlock()
	ch <- connectResult{c, err}
	return c, err
}

// resolveAccountID：空/"default" 解析为默认账号（活跃或第一个）
func resolveAccountID(accountID string) string {
	if accountID == "" || accountID == "default" {
		return models.GetDefaultAccountID()
	}
	return accountID
}

// Get 获取账号的活跃网关连接；无连接时用 code 连接；
// code 过期失败时尝试用 openid 自动刷新 code 后重连（对齐 Node refreshYybCodeIfNeeded）
func (p *ClientPool) Get(accountID string) (*gw.Client, error) {
	accountID = resolveAccountID(accountID)
	if accountID == "" {
		return nil, fmt.Errorf("没有可用的账号，请先在账号页添加/切换")
	}
	if c := p.cached(accountID); c != nil && !c.IsClosed() {
		return c, nil
	}

	acc := models.GetAccountByID(accountID)
	if acc == nil {
		return nil, fmt.Errorf("账号 %s 不存在", accountID)
	}
	if acc.Code == "" {
		return nil, fmt.Errorf("账号 %s 未配置登录 code", accountID)
	}

	// 单飞连接：并发的 Get 调用共享这一次登录，避免自踢
	return p.connectLocked(acc)
}

// UpdateCodeAndRelink 更新账号 code 并重建连接
func (p *ClientPool) UpdateCodeAndRelink(accountID, code string) (*gw.Client, error) {
	acc := models.GetAccountByID(accountID)
	if acc == nil {
		return nil, fmt.Errorf("账号 %s 不存在", accountID)
	}
	acc.Code = code
	if _, err := models.AddOrUpdateAccount(*acc); err != nil {
		return nil, err
	}
	p.mu.Lock()
	if old, ok := p.m[accountID]; ok {
		old.Close()
		delete(p.m, accountID)
	}
	p.mu.Unlock()
	return p.Get(accountID)
}

// evict 移除并关闭某账号的连接（踢下线/删除账号：清零重连状态）
func (p *ClientPool) evict(accountID string) {
	p.mu.Lock()
	if old, ok := p.m[accountID]; ok {
		old.Close()
		delete(p.m, accountID)
	}
	// 对齐 Node：踢下线/删除账号时清零重连计数与状态
	delete(p.offlineSince, accountID)
	delete(p.reconnectAttempts, accountID)
	delete(p.stopped, accountID)
	delete(p.kickBackoffUntil, accountID)
	p.mu.Unlock()
}

// UpdateClientVersion 热更新所有已连接账号的客户端版本号（保存系统配置后秒级生效，对齐 Node config_sync，无需重启）
func (p *ClientPool) UpdateClientVersion(v string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.m {
		if c != nil {
			c.SetClientVersion(v)
		}
	}
}

// Close 关闭全部连接
func (p *ClientPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.m {
		if c != nil {
			c.Close()
		}
	}
	p.m = map[string]*gw.Client{}
}

// loadAssetsAsync 后台异步拉取背包资产（点券/金豆），失败不影响主流程
func loadAssetsAsync(accountID string, c *gw.Client) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = c.FetchBagAssets(ctx)
		initStats(accountID, c.Gold(), c.Exp(), c.Coupon())
		updateStats(accountID, c.Gold(), c.Exp())
	}()
}
