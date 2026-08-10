package httpapi

import (
	"crypto/rand"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

const cookieName = "yyb_go_session"

// 通过环境变量配置，空值表示未设置
var (
	adminUser = os.Getenv("YYB_ADMIN_USER")
	adminPass = os.Getenv("YYB_ADMIN_PASS")
	ApiToken  = os.Getenv("YYB_API_TOKEN")
)

type sessionEntry struct {
	user      string
	expiresAt time.Time
}

var (
	sessions   = map[string]sessionEntry{}
	sessionsMu sync.Mutex
)

func sessionCleanupLoop() {
	for range time.NewTicker(5 * time.Minute).C {
		sessionsMu.Lock()
		now := time.Now()
		for k, s := range sessions {
			if now.After(s.expiresAt) {
				delete(sessions, k)
			}
		}
		sessionsMu.Unlock()
	}
}

func hexEncode(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0x0f]
	}
	return string(out)
}

func generateSessionID() string {
	b := make([]byte, 32)
	_, _ = io.ReadFull(rand.Reader, b)
	return hexEncode(b)
}

func requireAuth(w http.ResponseWriter, r *http.Request) bool {
	// Paths that don't need auth
	if r.URL.Path == "/login" || r.URL.Path == "/health" || r.URL.Path == "/token" {
		return true
	}

	// Check Bearer token for API calls
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " && auth[7:] == ApiToken {
		return true
	}

	// Fall back to session cookie (for browser)
	c, err := r.Cookie(cookieName)
	if err == nil {
		sessionsMu.Lock()
		s, ok := sessions[c.Value]
		sessionsMu.Unlock()
		if ok && !time.Now().After(s.expiresAt) {
			return true
		}
	}

	// API calls without token -> 401
	if len(r.URL.Path) >= 6 && (r.URL.Path[:6] == "/wxapp" || r.URL.Path[:9] == "/accounts" || r.URL.Path[:4] == "/qr" || r.URL.Path[:4] == "/docs") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"code":401,"msg":"invalid api token","data":null}`))
		return false
	}

	// Browser -> redirect to login
	http.Redirect(w, r, "/login", http.StatusFound)
	return false
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requireAuth(w, r) {
			next.ServeHTTP(w, r)
		}
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		errFlag := "none"
		if r.URL.Query().Get("err") == "1" {
			errFlag = "block"
		}
		w.Write([]byte(`<!doctype html><html lang="zh-CN"><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>YYB Go - 登录</title>
<style>
*{box-sizing:border-box}html,body{height:100%;margin:0}
body{display:grid;place-items:center;background:oklch(0.974 0.004 250);font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI','Microsoft YaHei',sans-serif}
.card{width:min(380px,calc(100vw-32px));background:white;border:1px solid oklch(0.885 0.012 250);border-radius:12px;padding:32px}
h1{margin:0 0 24px;font-size:22px;text-align:center}
label{display:block;margin-bottom:4px;font-size:13px;color:oklch(0.43 0.025 252)}
input{width:100%;padding:10px 12px;border:1px solid oklch(0.885 0.012 250);border-radius:8px;margin-bottom:16px;outline:none}
input:focus{border-color:oklch(0.54 0.205 3)}
button{width:100%;padding:10px;border:0;border-radius:8px;background:oklch(0.54 0.205 3);color:white;font-size:15px}
button:hover{background:oklch(0.48 0.195 3)}
.err{color:oklch(0.55 0.18 25);font-size:13px;margin:12px 0 0;text-align:center;display:` + errFlag + `}
</style>
<div class="card">
<h1>YYB Go</h1>
<form method="post" action="/login">
<label for="u">用户名</label>
<input id="u" name="username" required autocomplete="username" autofocus>
<label for="p">密码</label>
<input id="p" type="password" name="password" required autocomplete="current-password">
<button type="submit">登录</button>
<p class="err">用户名或密码错误</p>
</form>
</div>
</body></html>`))
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	r.ParseForm()
	u := r.FormValue("username")
	p := r.FormValue("password")

	if u != adminUser || p != adminPass {
		http.Redirect(w, r, "/login?err=1", http.StatusFound)
		return
	}

	sid := generateSessionID()
	sessionsMu.Lock()
	sessions[sid] = sessionEntry{user: u, expiresAt: time.Now().Add(24 * time.Hour)}
	sessionsMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}
