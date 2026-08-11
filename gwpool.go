package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/models"
)

// ClientPool 网关连接池：按账号缓存已登录的网关连接
type ClientPool struct {
	mu sync.Mutex
	m  map[string]*gw.Client // accountID -> 已登录连接

	// 掉线自动重连状态（accountID -> 状态）
	offlineSince     map[string]time.Time // 首次检测到断线的时间
	reconnectAttempts map[string]int      // 重连计数（对齐 Node：成功不清零，仅手动停止/踢下线/删除账号时清零）
	stopped           map[string]bool     // 达上限后停止自动重连，直到手动触发/重新连上
}

var clientPool = &ClientPool{
	m:                 map[string]*gw.Client{},
	offlineSince:      map[string]time.Time{},
	reconnectAttempts: map[string]int{},
	stopped:           map[string]bool{},
}

func gwConfig(platform string) gw.Config {
	if platform == "" {
		platform = "qq"
	} else if platform != "qq" && platform != "wx" {
		// YYB 扫码等渠道本质是 WX 渠道（对标 Node platform='wx'）
		platform = "wx"
	}
	return gw.Config{
		ServerURL:       "wss://gate-obt.nqf.qq.com/prod/ws",
		ClientVersion:   "1.13.0.4_20260723",
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
	p.mu.Lock()
	p.m[accountID] = c
	p.mu.Unlock()
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

	c, err := connect(acc)
	if err == nil {
		loadAssetsAsync(accountID, c)
		p.store(accountID, c)
		return c, nil
	}

	// code 过期 → 使用持久 openid 自动刷新重试
	if newCode, cerr := refreshCodeFromYyb(acc); cerr == nil && newCode != "" {
		acc.Code = newCode
		models.AddOrUpdateAccount(*acc)
		if c2, err2 := connect(acc); err2 == nil {
			loadAssetsAsync(accountID, c2)
			p.store(accountID, c2)
			return c2, nil
		}
	}
	return nil, err
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
	p.mu.Unlock()
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
