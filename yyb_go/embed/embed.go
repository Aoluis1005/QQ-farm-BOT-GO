// Package embed 提供将 YYB(应用宝) 服务内嵌到宿主进程的薄封装层。
// 它只是在 yyb_go/internal/httpapi 之上包了一层：在 127.0.0.1 随机端口启动
// HTTP 服务，并把 bot 所需的 Config / Start / GetApiToken / SetApiToken /
// DefaultDBFilename API 透传出去。真正的 YYB 协议实现（换 code、扫码登录、
// 账号同步等）全部在 internal/httpapi 内，本包不做任何自行实现的业务逻辑。
package embed

import (
	"context"
	"net"
	"net/http"
	"time"

	"yyb_go/internal/httpapi"
)

// DefaultDBFilename 与 httpapi 保持一致，bot 直接透传。
const DefaultDBFilename = httpapi.DefaultDBFilename

// Config 是宿主进程传入的精简配置。字段与 bot main.go 的 embed.Config 一一对应。
// 注意：不含 TCPProxy，内嵌模式下默认走直连。
type Config struct {
	ResourceRoot   string
	DBFilename     string
	SessionTTL     time.Duration
	RequestTimeout time.Duration
	AvatarTimeout  time.Duration
	ScanTimeout    time.Duration
	QRSessionTTL   time.Duration
}

// Server 是已启动的内嵌 YYB 服务实例。
type Server struct {
	baseURL string
	srv     *http.Server
	// Close 由 Start 赋值，类型为 func() error，bot 直接持有并调用。
	Close func() error
}

// BaseURL 返回内嵌服务监听的本地地址，例如 http://127.0.0.1:54321。
func (s *Server) BaseURL() string {
	return s.baseURL
}

// GetApiToken 返回当前鉴权 token（与 httpapi.ApiToken 同步）。
func GetApiToken() string {
	return httpapi.ApiToken
}

// SetApiToken 设置鉴权 token，内置服务鉴权与 bot 调用使用同一值。
func SetApiToken(t string) {
	httpapi.ApiToken = t
}

// Start 在 127.0.0.1 随机端口启动内嵌 YYB 服务，返回 Server 实例。
// 返回的 Server.BaseURL() 为服务地址，Server.Close 为优雅关闭函数。
func Start(cfg Config) (*Server, error) {
	hcfg := httpapi.Config{
		ResourceRoot:   cfg.ResourceRoot,
		DBFilename:     cfg.DBFilename,
		SessionTTL:     cfg.SessionTTL,
		RequestTimeout: cfg.RequestTimeout,
		AvatarTimeout:  cfg.AvatarTimeout,
		ScanTimeout:    cfg.ScanTimeout,
		QRSessionTTL:   cfg.QRSessionTTL,
	}

	app, err := httpapi.NewApp(hcfg)
	if err != nil {
		return nil, err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = app.Close()
		return nil, err
	}

	srv := &http.Server{
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		_ = srv.Serve(ln)
	}()

	baseURL := "http://" + ln.Addr().String()

	closeFn := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		shutdownErr := srv.Shutdown(ctx)
		_ = app.Close()
		return shutdownErr
	}

	return &Server{
		baseURL: baseURL,
		srv:     srv,
		Close:   closeFn,
	}, nil
}
