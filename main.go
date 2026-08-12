package main

import (
	"crypto/rand"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	goembed "embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Aoluis1005/go-farm-bot/models"
	"yyb_go/embed"
)

var dataDir string

// 前端 Vue 构建产物（web/dist）在编译期嵌入二进制，部署只需换二进制。
//
//go:embed web/dist
var webDistFS goembed.FS

// 内置应用宝(YYB)服务：BOT 自带换 code 能力，不依赖外部 YYB_API_URL/YYB_API_KEY。
// embeddedYybBaseURL 非空时表示内置服务已成功监听 127.0.0.1 随机端口。
var (
	embeddedYybBaseURL string
	embeddedYybClose   func() error
)

// newEmbeddedYybServer 初始化并启动内置 YYB http 服务，返回 baseURL 与清理函数。
// 监听 127.0.0.1 随机端口，ResourceRoot 指向运行目录下的 yyb-resource/。
func newEmbeddedYybServer() (string, func() error, error) {
	// 内置服务鉴权需要一个非空 token：未通过 YYB_API_TOKEN 配置时生成一个。
	// embed 门面对 httpapi.ApiToken 提供 Get/Set，内置服务鉴权与 BOT 调用使用同一值。
	if embed.GetApiToken() == "" {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return "", nil, err
		}
		embed.SetApiToken("yyb-" + hex.EncodeToString(buf))
	}

	srv, err := embed.Start(embed.Config{
		ResourceRoot:   "yyb-resource",
		DBFilename:     embed.DefaultDBFilename,
		SessionTTL:     30 * time.Minute,
		RequestTimeout: 45 * time.Second,
		AvatarTimeout:  10 * time.Second,
		ScanTimeout:    180 * time.Second,
		QRSessionTTL:   5 * time.Minute,
	})
	if err != nil {
		return "", nil, err
	}
	return srv.BaseURL(), srv.Close, nil
}

func main() {
	homeDir, _ := os.UserHomeDir()
	dataDir = homeDir + "/.qq-farm-bot"
	models.InitStore(dataDir)

	gwCfg := models.GetGatewayConfig()
	adminPort := gwCfg.AdminPort
	if adminPort == 0 {
		adminPort = 3007
	}

	mux := http.NewServeMux()

	// 启动内置应用宝(YYB)服务：BOT 自带换 code 能力。失败仅告警不阻塞 BOT 启动。
	if base, closeFn, err := newEmbeddedYybServer(); err != nil {
		log.Printf("[yyb] warn: failed to init embedded yyb service: %v", err)
	} else {
		embeddedYybBaseURL = base
		embeddedYybClose = closeFn
		log.Printf("[yyb] embedded yyb service listening at %s", base)
	}

	// 启动后台掉线自动重连扫描（增强：不破坏现有懒重连，Get() 仍可用）
	clientPool.StartAutoReconnect(context.Background())

	// 启动神秘商人自动购买（对齐 Node mystery-scheduler.js，60 分钟一轮）
	startMysteryAutoBuyLoop(context.Background())

	// 启动时为所有已配置账号建立初始连接（对齐 Node 启动即 startWorker，保证账号列表在线状态准确）。
	// 后台异步执行，不阻塞 HTTP 启动；失败仅记录（重连/懒连接兜底）。
	go func() {
		for _, acc := range models.GetAccounts() {
			if _, err := clientPool.Get(acc.ID); err != nil {
				log.Printf("[startup] 账号 %s 初始连接失败: %v", acc.ID, err)
			}
		}
	}()

	// 启动所有账号的自动化引擎（对齐 Node 启动即 startWorker）
	startAllAutomation()

	// 启动时扫描 game-config/seed_images_named，建立 itemId → 图片URL 映射（working dir 为游戏配置根目录）
	InitImageMap("game-config")
	initGameConfig("game-config")

	corsHandler := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, x-account-id, x-admin-token")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	api := http.NewServeMux()
	api.HandleFunc("/api/health", handleHealth)
	registerHomeAPI(api)
	registerProfileAPI(api)
	registerCareerAPI(api)
	registerAccountAPI(api)
	registerFriendOpsAPI(api)
	registerFriendExtraAPI(api)
	registerReconnectAPI(api)
	registerSettingsAPI(api)
	registerIllustratedAPI(api)
	registerAnalyticsAPI(api)
	registerActivityAPI(api)
	registerShopAPI(api)
	registerTaskAPI(api)
	registerAdminAuthAPI(api)

	mux.Handle("/api/", corsHandler(adminAuthMiddleware(api)))

	// 游戏静态资源（种子/作物图片等）：/game-config/** → game-config/ 目录
	mux.Handle("/game-config/", http.StripPrefix("/game-config/", http.FileServer(http.Dir("game-config"))))

	// 根路径：serve 嵌入的前端静态产物（Vue 构建的 web/dist），带 SPA history 回退。
	distSub, err := fs.Sub(webDistFS, "web/dist")
	if err != nil {
		log.Fatalf("[admin] 嵌入前端失败: %v", err)
	}
	distFileServer := http.FileServer(http.FS(distSub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		// 资源文件存在则直接返回（带正确 Content-Type）
		if p != "" {
			if f, openErr := distSub.Open(p); openErr == nil {
				f.Close()
				distFileServer.ServeHTTP(w, r)
				return
			}
		}
		// 否则回退到 index.html（客户端路由 /farm、/shop 等直接刷新可命中）
		idx, readErr := webDistFS.ReadFile("web/dist/index.html")
		if readErr != nil {
			http.Error(w, "前端未构建", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(idx)
	})

	addr := fmt.Sprintf("0.0.0.0:%d", adminPort)
	log.Printf("[admin] QQ Farm Bot Go · http://localhost:%d", adminPort)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[admin] failed to start: %v", err)
	}
	// 主循环退出时（正常路径 main 不会返回）关闭内置 YYB 服务
	if embeddedYybClose != nil {
		_ = embeddedYybClose()
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"ok": true, "status": "ok", "uptime": time.Since(startTime).Seconds(),
	})
}

var startTime = time.Now()

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": msg})
}
