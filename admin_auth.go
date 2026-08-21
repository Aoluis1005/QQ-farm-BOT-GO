package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/models"
)

// ============================================================
// 后台管理员鉴权（自用）
//  - 登录密码：优先已持久化 passwordHash，其次环境变量 ADMIN_PASSWORD
//  - 未设置密码时，后台处于"未初始化"态，仅允许首次设置密码(/api/admin/setup)
//  - token 存内存(重启需重新登录)，
//  - 中间件统一做：panic 兜底 + 鉴权 + 超时守卫
// ============================================================

const adminTokenTTL = 24 * time.Hour

var (
	adminTokensMu sync.Mutex
	adminTokens   = map[string]time.Time{}
)

// ---- 密码哈希（随机 salt + sha256，constant-time 比对） ----

func adminHashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := sha256.Sum256(append(salt, []byte(password)...))
	return hex.EncodeToString(salt) + "$" + hex.EncodeToString(sum[:]), nil
}

func adminVerifyPassword(password, stored string) bool {
	if stored == "" {
		return false
	}
	parts := strings.SplitN(stored, "$", 2)
	if len(parts) != 2 {
		return false
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sum := sha256.Sum256(append(salt, []byte(password)...))
	return subtle.ConstantTimeCompare([]byte(parts[1]), []byte(hex.EncodeToString(sum[:]))) == 1
}

func adminEnvPassword() string { return strings.TrimSpace(os.Getenv("ADMIN_PASSWORD")) }

// adminHasPassword 是否已设置管理员密码（含环境变量）
func adminHasPassword() bool {
	if models.GetAdminPasswordHash() != "" {
		return true
	}
	return adminEnvPassword() != ""
}

// adminPasswordMatches 校验密码：优先持久化 hash，其次环境变量明文
func adminPasswordMatches(password string) bool {
	if h := models.GetAdminPasswordHash(); h != "" {
		return adminVerifyPassword(password, h)
	}
	if env := adminEnvPassword(); env != "" {
		return subtle.ConstantTimeCompare([]byte(password), []byte(env)) == 1
	}
	return false
}

// ---- token 管理 ----

func adminNewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	adminTokensMu.Lock()
	adminTokens[tok] = time.Now().Add(adminTokenTTL)
	adminTokensMu.Unlock()
	return tok, nil
}

func adminTokenValid(tok string) bool {
	if tok == "" {
		return false
	}
	adminTokensMu.Lock()
	defer adminTokensMu.Unlock()
	exp, ok := adminTokens[tok]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(adminTokens, tok)
		return false
	}
	return true
}

func adminTokenRevoke(tok string) {
	adminTokensMu.Lock()
	delete(adminTokens, tok)
	adminTokensMu.Unlock()
}

func adminTokenFromRequest(r *http.Request) string {
	if t := strings.TrimSpace(r.Header.Get("x-admin-token")); t != "" {
		return t
	}
	if a := strings.TrimSpace(r.Header.Get("Authorization")); a != "" {
		if strings.HasPrefix(a, "Bearer ") {
			return strings.TrimSpace(a[len("Bearer "):])
		}
	}
	return ""
}

// ---- Admin API 路由 ----

func registerAdminAuthAPI(api *http.ServeMux) {
	api.HandleFunc("/api/admin/status", handleAdminStatus)
	api.HandleFunc("/api/admin/login", handleAdminLogin)
	api.HandleFunc("/api/admin/setup", handleAdminSetup)
	api.HandleFunc("/api/admin/change-password", handleAdminChangePassword)
	api.HandleFunc("/api/admin/logout", handleAdminLogout)
	api.HandleFunc("/api/admin/system-config", handleAdminSystemConfig)
}

// status: 后台鉴权状态（是否已初始化密码）
func handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"ok": true, "hasPassword": adminHasPassword(),
	})
}

// login: 登录，成功返回 token
func handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "参数错误")
		return
	}
	if body.Password == "" {
		writeError(w, 400, "请输入密码")
		return
	}
	if !adminPasswordMatches(body.Password) {
		writeError(w, 401, "密码错误")
		return
	}
	tok, err := adminNewToken()
	if err != nil {
		writeError(w, 500, "生成凭证失败")
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "token": tok})
}

// setup: 首次设置密码（仅当从未设置过密码时可用）
func handleAdminSetup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "参数错误")
		return
	}
	if adminHasPassword() {
		writeError(w, 409, "已设置过密码，请直接登录或修改密码")
		return
	}
	if len(body.Password) < 6 {
		writeError(w, 400, "密码至少 6 位")
		return
	}
	h, err := adminHashPassword(body.Password)
	if err != nil {
		writeError(w, 500, "设置失败")
		return
	}
	if err := models.SetAdminPasswordHash(h); err != nil {
		writeError(w, 500, "保存失败")
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

// change-password: 修改密码（需已登录）
func handleAdminChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "参数错误")
		return
	}
	if !adminPasswordMatches(body.OldPassword) {
		writeError(w, 401, "原密码错误")
		return
	}
	if len(body.NewPassword) < 6 {
		writeError(w, 400, "新密码至少 6 位")
		return
	}
	h, err := adminHashPassword(body.NewPassword)
	if err != nil {
		writeError(w, 500, "修改失败")
		return
	}
	if err := models.SetAdminPasswordHash(h); err != nil {
		writeError(w, 500, "保存失败")
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	adminTokenRevoke(adminTokenFromRequest(r))
	writeJSON(w, map[string]interface{}{"ok": true})
}

// handleAdminSystemConfig 读取/更新系统配置（客户端版本号热更新）
func handleAdminSystemConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, map[string]interface{}{"ok": true, "data": models.GetSystemConfig()})
		return
	case "POST":
		var body struct {
			ClientVersion          string `json:"clientVersion"`
			OfflineNotifyEnabled   *bool  `json:"offlineNotifyEnabled"`
			OfflineNotifyNick      string `json:"offlineNotifyNick"`
			OfflineNotifyCooldownMin int  `json:"offlineNotifyCooldownMin"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, 400, "参数错误")
			return
		}
		sc := models.GetSystemConfig()
		if cv := strings.TrimSpace(body.ClientVersion); cv != "" {
			sc.ClientVersion = cv
		}
		if body.OfflineNotifyEnabled != nil {
			sc.OfflineNotifyEnabled = *body.OfflineNotifyEnabled
		}
		sc.OfflineNotifyNick = body.OfflineNotifyNick
		if body.OfflineNotifyCooldownMin > 0 {
			sc.OfflineNotifyCooldownMin = body.OfflineNotifyCooldownMin
		}
		if err := models.SetSystemConfig(sc); err != nil {
			writeError(w, 500, "保存失败: "+err.Error())
			return
		}
		// 热更新所有已连接账号（秒级生效，无需重启）
		clientPool.UpdateClientVersion(sc.ClientVersion)
		writeJSON(w, map[string]interface{}{"ok": true, "data": models.GetSystemConfig()})
		return
	default:
		writeError(w, 405, "method not allowed")
	}
}

// ---- apiPublicPath: 免鉴权的公开路径 ----

func apiPublicPath(path string) bool {
	switch path {
	case "/api/health", "/api/admin/status", "/api/admin/login", "/api/admin/setup":
		return true
	}
	return false
}

// adminAuthMiddleware 包装 api：panic 兜底 + 鉴权 + 超时注入
func adminAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. panic 兜底：单个 handler 崩溃不拖垮整个进程
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[admin] panic recovered: %v", rec)
				writeError(w, 500, "服务器内部错误")
			}
		}()

		path := r.URL.Path
		if !apiPublicPath(path) {
			// 2. 鉴权
			if !adminHasPassword() {
				writeError(w, 401, "请先设置管理员密码")
				return
			}
			if !adminTokenValid(adminTokenFromRequest(r)) {
				writeError(w, 401, "未登录或登录已过期")
				return
			}
		}

		// 3. 超时守卫：注入 context 超时（health 除外）
		if path != "/api/health" {
			ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// requireConfirmed 危险操作二次确认（body 需带 confirmed:true）
func requireConfirmed(r *http.Request) bool {
	var body struct {
		Confirmed bool `json:"confirmed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return false
	}
	return body.Confirmed
}
