package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Aoluis1005/go-farm-bot/models"
	"yyb_go/embed"
)

func registerAccountAPI(mux *http.ServeMux) {
	// 账号 CRUD
	mux.HandleFunc("/api/accounts", handleAccounts)
	mux.HandleFunc("/api/accounts/", handleAccountByID)
	mux.HandleFunc("/api/accounts/active", handleActiveAccount)
	// YYB 代理（扫码登录走应用宝，QQ 小程序扫码已废弃不实现）
	mux.HandleFunc("/api/yyb/accounts", handleYybAccounts)
	mux.HandleFunc("/api/yyb/getcode", handleYybGetCode)
	mux.HandleFunc("/api/yyb/thirdparty-code", handleYybThirdpartyCode)
	mux.HandleFunc("/api/yyb/qr/create", handleYybQRCreate)
	mux.HandleFunc("/api/yyb/qr/poll", handleYybQRPoll)
	mux.HandleFunc("/api/yyb/qr/confirm", handleYybQRConfirm)
}

// ---- 账号管理 ----

func handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		accounts := models.GetAccounts()
		// 在线状态用网关连接实时判断（对齐 Node getAccounts：acc.running = !!worker）——
		// 持久化 status 创建即 offline 且从不更新，故这里覆盖返回，不写库。
		for i := range accounts {
			if c := clientPool.cached(accounts[i].ID); c != nil && !c.IsClosed() {
				accounts[i].Status = "online"
			} else {
				accounts[i].Status = "offline"
			}
		}
		writeJSON(w, map[string]interface{}{"ok": true, "data": accounts})
	case "POST":
		var body struct {
			Name     string `json:"name"`
			Code     string `json:"code"`
			Platform string `json:"platform"` // "qq" / "wx"
			QQ       string `json:"qq"`
			UIN      string `json:"uin"`
			GID      string `json:"gid"`
			OpenID   string `json:"openId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, 400, "invalid body")
			return
		}
		if body.Code == "" {
			writeError(w, 400, "code 不能为空")
			return
		}
		if body.Platform == "" {
			body.Platform = "qq"
		}
		id := fmt.Sprintf("%d", time.Now().UnixNano())
		acc := models.Account{
			ID:        id,
			Name:      body.Name,
			Code:      body.Code,
			Platform:  body.Platform,
			QQ:       body.QQ,
			UIN:      body.UIN,
			GID:      body.GID,
			OpenID:   body.OpenID,
			Status:   "offline",
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		if acc.Name == "" {
			acc.Name = "新账号"
		}
		result, err := models.AddOrUpdateAccount(acc)
		if err != nil {
			writeError(w, 500, "添加账号失败: "+err.Error())
			return
		}
		// 默认把最新添加的账号设为当前活跃账号，便于立即查看真实数据
		models.SetActiveAccount(acc.ID)
		// 同步连接网关，连好再返回，前端添加后刷新即有数据
		connected := false
		if c, cerr := clientPool.Get(acc.ID); cerr == nil && c != nil && c.GID != 0 {
			connected = true
		}
		writeJSON(w, map[string]interface{}{"ok": true, "data": result, "activeAccountId": acc.ID, "connected": connected})
	default:
		writeError(w, 405, "method not allowed")
	}
}


// handleActiveAccount 获取/切换当前活跃账号
func handleActiveAccount(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, map[string]interface{}{"ok": true, "data": map[string]interface{}{
			"accountId": models.GetActiveAccountID(),
		}})
	case "POST":
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, 400, "invalid body")
			return
		}
		if body.ID == "" {
			writeError(w, 400, "缺少账号 id")
			return
		}
		if models.GetAccountByID(body.ID) == nil {
			writeError(w, 404, "账号不存在")
			return
		}
		if err := models.SetActiveAccount(body.ID); err != nil {
			writeError(w, 500, "切换失败: "+err.Error())
			return
		}
		// 同步连接网关（首次需几秒，确保切换后数据立即可用）
		connected := false
		if c, cerr := clientPool.Get(body.ID); cerr == nil && c != nil && c.GID != 0 {
			connected = true
		}
		writeJSON(w, map[string]interface{}{"ok": true, "accountId": body.ID, "connected": connected})
	default:
		writeError(w, 405, "method not allowed")
	}
}

func handleAccountByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/accounts/")
	if id == "" {
		writeError(w, 400, "missing account id")
		return
	}
	var body struct {
		Name     string `json:"name"`
		Code     string `json:"code"`
		Platform string `json:"platform"`
	}
	if r.Method == "PUT" || r.Method == "POST" {
		json.NewDecoder(r.Body).Decode(&body)
	}
	switch r.Method {
	case "DELETE":
		if err := models.DeleteAccount(id); err != nil {
			writeError(w, 404, "account not found")
			return
		}
		// 删除的是当前活跃账号时清空，回退到第一个账号
		if models.GetActiveAccountID() == id {
			models.SetActiveAccount("")
		}
		clientPool.evict(id)
		writeJSON(w, map[string]interface{}{"ok": true})
	case "PUT", "POST":
		acc := models.GetAccountByID(id)
		if acc == nil {
			writeError(w, 404, "account not found")
			return
		}
		if body.Name != "" {
			acc.Name = body.Name
		}
		if body.Platform != "" {
			acc.Platform = body.Platform
		}
		relink := body.Code != "" && body.Code != acc.Code
		if body.Code != "" {
			acc.Code = body.Code
		}
		if _, err := models.AddOrUpdateAccount(*acc); err != nil {
			writeError(w, 500, "保存失败: "+err.Error())
			return
		}
		if relink {
			clientPool.UpdateCodeAndRelink(id, acc.Code)
		}
		writeJSON(w, map[string]interface{}{"ok": true, "accountId": id, "relinked": relink})
	default:
		writeError(w, 405, "method not allowed")
	}
}

// ---- YYB 应用宝代理（对标 Node admin-yyb-routes.js） ----

func resolveYybCreds(body map[string]interface{}) (apiBase, apiKey string) {
	// 优先取请求体传入，未传则回退到源码/环境变量（对标 Node resolveYybCreds）
	if v, ok := body["apiBase"].(string); ok && strings.TrimSpace(v) != "" {
		apiBase = strings.TrimRight(v, "/")
		apiBase = strings.TrimLeft(apiBase, "/")
		apiBase = strings.Replace(apiBase, "/wxapp/getCode", "", 1)
		apiBase = strings.Replace(apiBase, "/wxapp", "", 1)
		apiBase = strings.Replace(apiBase, "/accounts", "", 1)
	} else {
		apiBase = strings.TrimRight(os.Getenv("YYB_API_URL"), "/")
	}
	if v, ok := body["apiKey"].(string); ok && strings.TrimSpace(v) != "" {
		apiKey = strings.TrimSpace(v)
	} else {
		apiKey = os.Getenv("YYB_API_KEY")
	}
	// 内置 YYB 服务兜底：未配置外部 YYB_API_URL/YYB_API_KEY 且内置服务可用时，走内置服务。
	// apiKey 用内置服务的 ApiToken（与内置 httpapi 鉴权一致）。
	if embeddedYybBaseURL != "" {
		if strings.TrimSpace(apiBase) == "" {
			apiBase = embeddedYybBaseURL
		}
		if strings.TrimSpace(apiKey) == "" {
			apiKey = embed.GetApiToken()
		}
	}
	return
}

func callYybAPI(apiBase, path, apiKey string, method string, bodyObj interface{}) (map[string]interface{}, error) {
	if apiBase == "" || apiKey == "" {
		return nil, fmt.Errorf("应用宝接口地址或 API Token 未配置")
	}

	fullURL := apiBase + path
	var bodyBytes []byte
	if bodyObj != nil {
		var err error
		bodyBytes, err = json.Marshal(bodyObj)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败")
		}
	}

	req, _ := http.NewRequest(method, fullURL, bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	bodyText, _ := io.ReadAll(resp.Body)
	var data map[string]interface{}
	if err := json.Unmarshal(bodyText, &data); err != nil {
		return nil, fmt.Errorf("应用宝接口返回非 JSON")
	}

	// 统一 envelope: { code: 0, msg, data }
	if code, ok := data["code"]; ok {
		if codeNum, ok := code.(float64); ok && codeNum != 0 {
			msg := ""
			if m, ok := data["msg"].(string); ok {
				msg = m
			}
			return nil, fmt.Errorf("应用宝接口错误 code=%v: %s", codeNum, msg)
		}
	}
	return data, nil
}

func handleYybAccounts(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	json.NewDecoder(r.Body).Decode(&body)
	apiBase, apiKey := resolveYybCreds(body)

	result, err := callYybAPI(apiBase, "/accounts", apiKey, "GET", nil)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	accounts, _ := result["data"].([]interface{})
	if accounts == nil {
		accounts = []interface{}{}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": accounts})
}

func handleYybGetCode(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	json.NewDecoder(r.Body).Decode(&body)
	apiBase, apiKey := resolveYybCreds(body)

	openid, _ := body["openid"].(string)
	if openid == "" {
		writeError(w, 400, "缺少 openid")
		return
	}
	appID, _ := body["appId"].(string)
	if appID == "" {
		appID = "wx5306c5978fdb76e4"
	}

	code, err := getCodeFromYyb(apiBase, apiKey, openid, appID)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": map[string]interface{}{
		"code":   code,
		"openid": openid,
	}})
}

// getCodeFromYyb 用 openid 调应用宝 /wxapp/getCode 换新 code（对齐 Node refreshYybCodeIfNeeded）
func getCodeFromYyb(apiBase, apiKey, openid, appID string) (string, error) {
	if apiBase == "" || apiKey == "" {
		return "", fmt.Errorf("应用宝接口地址或 API Token 未配置")
	}
	if appID == "" {
		appID = "wx5306c5978fdb76e4"
	}
	result, err := callYybAPI(apiBase, "/wxapp/getCode", apiKey, "POST", map[string]interface{}{
		"ref":    openid,
		"app_id": appID,
	})
	if err != nil {
		return "", err
	}
	innerData, _ := result["data"].(map[string]interface{})
	if innerData != nil {
		if innerResult, ok := innerData["result"].(map[string]interface{}); ok {
			if c, ok := innerResult["code"].(string); ok && c != "" {
				return c, nil
			}
		}
	}
	return "", fmt.Errorf("应用宝接口未返回 code")
}

func handleYybThirdpartyCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIBase      string `json:"apiBase"`
		APIToken     string `json:"apiToken"`
		OpenID       string `json:"openid"`
		ForceRefresh bool   `json:"forceRefresh"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid body")
		return
	}
	if body.APIBase == "" || body.APIToken == "" || body.OpenID == "" {
		writeError(w, 400, "缺少 apiBase/apiToken/openid")
		return
	}

	// 调用第三方接口: POST {apiBase}/api/open/v1/farm/code
	fullURL := strings.TrimRight(body.APIBase, "/") + "/api/open/v1/farm/code"
	reqBody := map[string]interface{}{
		"openid":       body.OpenID,
		"forceRefresh": body.ForceRefresh,
	}
	reqBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", fullURL, bytes.NewReader(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+body.APIToken)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, 500, "第三方接口请求失败: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		writeError(w, 401, "第三方 API Token 无效")
		return
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	// 提取 code（兼容多种信封格式）
	code := extractCodeFromThirdparty(result)
	if code == "" {
		writeError(w, 500, "第三方接口未返回 code")
		return
	}

	openid, _ := result["openid"].(string)
	if openid == "" {
		if data, ok := result["data"].(map[string]interface{}); ok {
			if o, ok := data["openid"].(string); ok {
				openid = o
			}
		}
	}
	if openid == "" {
		openid = body.OpenID
	}

	writeJSON(w, map[string]interface{}{
		"ok": true,
		"data": map[string]interface{}{
			"code":   code,
			"openid": openid,
		},
	})
}

func extractCodeFromThirdparty(data map[string]interface{}) string {
	// 尝试多种路径提取 code
	if inner, ok := data["data"].(map[string]interface{}); ok {
		if c, ok := inner["code"].(string); ok && len(c) >= 4 {
			return c
		}
		if innerResult, ok := inner["result"].(map[string]interface{}); ok {
			if c, ok := innerResult["code"].(string); ok && len(c) >= 4 {
				return c
			}
		}
		if c, ok := inner["token"].(string); ok && len(c) >= 4 {
			return c
		}
	}
	if c, ok := data["code"].(string); ok && len(c) >= 4 {
		return c
	}
	if c, ok := data["token"].(string); ok && len(c) >= 4 {
		return c
	}
	return ""
}

// ---- YYB QR 扫码登录 ----

func handleYybQRCreate(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	json.NewDecoder(r.Body).Decode(&body)
	apiBase, apiKey := resolveYybCreds(body)

	result, err := callYybAPI(apiBase, "/qr?as_base64=true", apiKey, "POST", nil)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": result["data"]})
}

func handleYybQRPoll(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	json.NewDecoder(r.Body).Decode(&body)
	apiBase, apiKey := resolveYybCreds(body)

	sessionID, _ := body["sessionId"].(string)
	if sessionID == "" {
		writeError(w, 400, "缺少 sessionId")
		return
	}

	result, err := callYybAPI(apiBase, "/qr/"+url.PathEscape(sessionID)+"/poll", apiKey, "GET", nil)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": result["data"]})
}

func handleYybQRConfirm(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	json.NewDecoder(r.Body).Decode(&body)
	apiBase, apiKey := resolveYybCreds(body)

	sessionID, _ := body["sessionId"].(string)
	if sessionID == "" {
		writeError(w, 400, "缺少 sessionId")
		return
	}

	result, err := callYybAPI(apiBase, "/qr/"+url.PathEscape(sessionID)+"/confirm", apiKey, "POST", nil)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": result["data"]})
}

// 确保 base64 包被使用
var _ = base64.StdEncoding
