package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/Aoluis1005/go-farm-bot/config"
	"github.com/Aoluis1005/go-farm-bot/models"
)

// ============================================================
// 掉线自动重连（对齐 Node core/src/runtime/worker-manager.js + utils/network.js）
// 状态机：online →(断线/心跳连续miss)→ offline →(延迟 reconnectDelayMin)→ 换code重连
//   → 成功 → online（重连成功**不清零**计数）
//   → 失败 → 计数保持，延迟后重试；调度前 current >= maxAttempts → stopped（停止）
// 计数仅在「手动停止 / 踢下线 / 删除账号」时清零。
// ============================================================

// StartAutoReconnect 启动后台自动重连扫描（在 main 初始化后调用一次）
func (p *ClientPool) StartAutoReconnect(ctx context.Context) {
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.scanAutoReconnect()
			}
		}
	}()
}

type reconnectTarget struct {
	id string
}

// scanAutoReconnect 扫描所有连接的断线状态；满足延迟条件的账号调度重连。
// 对齐 Node：autoReconnect=false 或 reconnectDelayMin<=0 → 直接不重连。
// 调度前读当前重连计数，current >= maxAttempts → 停止并删计数；否则 current+1 存入再执行。
func (p *ClientPool) scanAutoReconnect() {
	var toReconnect []reconnectTarget
	p.mu.Lock()
	now := time.Now()
	var toReset []string
	for id, c := range p.m {
		if c == nil {
			continue
		}
		cfg := models.GetAutoReconnect(id)
		if !cfg.Enabled || cfg.ReconnectDelayMin <= 0 {
			continue
		}
		if c.IsClosed() {
			if p.stopped[id] {
				continue
			}
			since, ok := p.offlineSince[id]
			if !ok {
				// 刚检测到断线：记录时间，等过延迟周期再重连（容忍瞬时抖动）
				appendOpLog(id, "掉线", "连接已断开")
				p.offlineSince[id] = now
				continue
			}
			if now.Sub(since) >= time.Duration(cfg.ReconnectDelayMin)*time.Minute {
				// 调度前检查计数（对齐 Node：先读 current，达到上限即停止并删计数）
				attempts := p.reconnectAttempts[id]
				if attempts >= cfg.ReconnectMaxAttempts {
					p.stopped[id] = true
					delete(p.reconnectAttempts, id)
					log.Printf("[reconnect] 账号 %s 已达最大重连次数(%d)，停止自动重连（可手动重试）", id, cfg.ReconnectMaxAttempts)
					continue
				}
				// current+1 存入，再执行重连
				p.reconnectAttempts[id] = attempts + 1
				toReconnect = append(toReconnect, reconnectTarget{id: id})
			}
		} else {
			// 连接健康：清除断线时间与停止态，但**保留重连计数**（对齐 Node 成功不清零）
			toReset = append(toReset, id)
		}
	}
	for _, id := range toReset {
		delete(p.offlineSince, id)
		delete(p.stopped, id)
	}
	p.mu.Unlock()

	// 锁外执行重连，避免占用池锁
	for _, tgt := range toReconnect {
		p.reconnectAccount(tgt.id)
	}
}

// reconnectAccount 对某账号执行一次重连：内置 YYB 换 code → 重建连接。
// 重连前检查：若已有活跃连接则跳过；账号已删除则取消并清计数。
func (p *ClientPool) reconnectAccount(accountID string) {
	// 重连前检查1：账号已删除 → 取消并清计数
	acc := models.GetAccountByID(accountID)
	if acc == nil {
		p.resetAutoReconnect(accountID)
		log.Printf("[reconnect] 账号 %s 已删除，取消自动重连并清计数", accountID)
		return
	}
	// 重连前检查2：已有活跃连接则跳过（无需重连）
	if c := p.cached(accountID); c != nil && !c.IsClosed() {
		p.mu.Lock()
		delete(p.offlineSince, accountID)
		delete(p.stopped, accountID)
		p.mu.Unlock()
		return
	}

	// 换 code（走内置 YYB）；失败用旧 code 继续（不改 code）
	newCode, cerr := refreshCodeFromYyb(acc)
	if cerr == nil && newCode != "" {
		acc.Code = newCode
		models.AddOrUpdateAccount(*acc)
	} else {
		log.Printf("[reconnect] 账号 %s 换 code 失败（将用旧 code 尝试）: %v", accountID, cerr)
	}

	// 单飞连接：同一账号同时只连一个，避免与前端 Get / onKick 并发登录造成自踢
	appendOpLog(accountID, "重连", "正在自动重连...")
	_, err := p.connectLocked(acc)
	if err != nil {
		// 封号（权限不足 code=1000016）：永久关闭该号自动重连，避免反复打无效登录。
		if gw.IsBanError(err) {
			p.disableAutoReconnectForBan(accountID)
			return
		}
		// 失败：计数已在调度时累加，这里保持计数，等待下一轮重试
		p.mu.Lock()
		att := p.reconnectAttempts[accountID]
		p.mu.Unlock()
		log.Printf("[reconnect] 账号 %s 重连失败(第%d次): %v", accountID, att, err)
		return
	}

	// 成功：清断线/停止态；**保留重连计数**（对齐 Node）。连接与资产已在 connectLocked 内完成。
	p.mu.Lock()
	delete(p.offlineSince, accountID)
	delete(p.stopped, accountID)
	p.mu.Unlock()
	appendOpLog(accountID, "重连", "自动重连成功")
	log.Printf("[reconnect] 账号 %s 自动重连成功", accountID)
}

// TryReconnectNow 手动触发立即重连（重置计数与停止态后执行，视为重新开始）
func (p *ClientPool) TryReconnectNow(accountID string) {
	cfg := models.GetAutoReconnect(accountID)
	if !cfg.Enabled {
		log.Printf("[reconnect] 账号 %s 自动重连未开启，跳过手动重连", accountID)
		return
	}
	if models.GetAccountByID(accountID) == nil {
		return
	}
	if c := p.cached(accountID); c != nil && !c.IsClosed() {
		log.Printf("[reconnect] 账号 %s 连接健康，无需重连", accountID)
		return
	}
	// 手动触发：先重置状态（对齐 Node 手动操作清零），再执行重连
	p.resetAutoReconnect(accountID)
	p.reconnectAccount(accountID)
}

// disableAutoReconnectForBan 账号被封(1000016)时永久关闭自动重连：写库 disabled + 标记 stopped，
// 使后台扫描与手动重试都不再登录该号（封号后任何登录都是无效且可能加重风险）。
func (p *ClientPool) disableAutoReconnectForBan(accountID string) {
	cfg := models.GetAutoReconnect(accountID)
	if cfg.Enabled {
		if serr := models.SetAutoReconnect(accountID, false, cfg.ReconnectDelayMin, cfg.ReconnectMaxAttempts); serr != nil {
			log.Printf("[reconnect] 账号 %s 保存封号停用配置失败: %v", accountID, serr)
		}
	}
	p.mu.Lock()
	p.stopped[accountID] = true
	delete(p.reconnectAttempts, accountID)
	p.mu.Unlock()
	log.Printf("[reconnect] 账号 %s 返回封号码(1000016)，已永久关闭自动重连", accountID)
	appendOpLog(accountID, "重连", "账号已被封禁(1000016)，已永久关闭自动重连")
}

// resetAutoReconnect 清零某账号的断线/重连计数/停止标记（手动触发或踢下线/删除调用）
func (p *ClientPool) resetAutoReconnect(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.offlineSince, accountID)
	delete(p.reconnectAttempts, accountID)
	delete(p.stopped, accountID)
}

// reconnectStatus 返回某账号自动重连运行状态（供前端展示）
func (p *ClientPool) reconnectStatus(accountID string) map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := "unknown"
	if c := p.m[accountID]; c != nil {
		if c.IsClosed() {
			state = "offline"
		} else {
			state = "online"
		}
	}
	st := map[string]interface{}{
		"state":     state,
		"stopped":   p.stopped[accountID],
		"attempts":  p.reconnectAttempts[accountID],
	}
	if since, ok := p.offlineSince[accountID]; ok {
		st["offlineSince"] = since.Format(time.RFC3339)
	}
	return st
}

// ============================================================
// 配置读写接口
// ============================================================

func registerReconnectAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/reconnect/config", handleReconnectConfig)
	mux.HandleFunc("/api/reconnect/retry", handleReconnectRetry)
}

func handleReconnectConfig(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		// 无账号时 GET 返回默认开启配置，便于前端初始化开关；写操作为避免误存仍报错
		if r.Method == http.MethodGet {
			def := config.DefaultAccountConfig().AutoReconnect
			writeJSON(w, map[string]interface{}{
				"ok": true,
				"data": map[string]interface{}{
					"enabled":              def.Enabled,
					"reconnectDelayMin":    def.ReconnectDelayMin,
					"reconnectMaxAttempts": def.ReconnectMaxAttempts,
				}, "state": nil,
			})
			return
		}
		writeError(w, 400, "没有可用账号")
		return
	}
	switch r.Method {
	case http.MethodGet:
		cfg := models.GetAutoReconnect(accountID)
		writeJSON(w, map[string]interface{}{
			"ok": true,
			"data": map[string]interface{}{
				"enabled":              cfg.Enabled,
				"reconnectDelayMin":    cfg.ReconnectDelayMin,
				"reconnectMaxAttempts": cfg.ReconnectMaxAttempts,
				"state":                clientPool.reconnectStatus(accountID),
			},
		})
	case http.MethodPut, http.MethodPost:
		var body struct {
			Enabled              *bool `json:"enabled"`
			ReconnectDelayMin    *int  `json:"reconnectDelayMin"`
			ReconnectMaxAttempts *int  `json:"reconnectMaxAttempts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, 400, "invalid body")
			return
		}
		cfg := models.GetAutoReconnect(accountID)
		enabled := cfg.Enabled
		if body.Enabled != nil {
			enabled = *body.Enabled
		}
		delay := cfg.ReconnectDelayMin
		if body.ReconnectDelayMin != nil {
			delay = *body.ReconnectDelayMin
		}
		maxAt := cfg.ReconnectMaxAttempts
		if body.ReconnectMaxAttempts != nil {
			maxAt = *body.ReconnectMaxAttempts
		}
		if err := models.SetAutoReconnect(accountID, enabled, delay, maxAt); err != nil {
			writeError(w, 500, "保存失败: "+err.Error())
			return
		}
		// 修改配置后重置计数/停止态（对齐 Node：手动操作清零）
		clientPool.resetAutoReconnect(accountID)
		writeJSON(w, map[string]interface{}{"ok": true, "accountId": accountID, "enabled": enabled})
	default:
		writeError(w, 405, "method not allowed")
	}
}

func handleReconnectRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeError(w, 400, "没有可用账号")
		return
	}
	clientPool.resetAutoReconnect(accountID)
	go clientPool.TryReconnectNow(accountID)
	writeJSON(w, map[string]interface{}{"ok": true, "accountId": accountID})
}
