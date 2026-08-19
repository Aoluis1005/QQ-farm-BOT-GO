package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// getCodeFromThirdpartyYyb 调用第三方应用宝的 {apiBase}/api/open/v1/farm/code 接口，
// 用 openid + Bearer APIToken 换取登录 code。失败返回 error；refresh=true 强制刷新。
// 重用 handleYybThirdpartyCode 中的多信封 code 提取逻辑（extractCodeFromThirdparty）。
//
// 注意：与内置 YYB 流程互不冲突；调用方（refreshCodeFromYyb）只在 acc.Thirdparty 三字段全填时走本函数。
func getCodeFromThirdpartyYyb(apiBase, apiToken, openid string, forceRefresh bool) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	// 规范化：去掉尾部可能误带的路径后缀（对齐 Node normalizeApiBase）
	base = stripSuffix(base, "/api/open/v1/farm/code")
	base = stripSuffix(base, "/api/open/v1")
	base = stripSuffix(base, "/api/open")
	if base == "" {
		return "", fmt.Errorf("第三方接口地址为空")
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return "", fmt.Errorf("第三方接口地址必须为 http/https")
	}
	if apiToken == "" {
		return "", fmt.Errorf("第三方 API Token 未配置")
	}
	if openid == "" {
		return "", fmt.Errorf("缺少 openid")
	}

	url := base + "/api/open/v1/farm/code"
	body, _ := json.Marshal(map[string]interface{}{
		"openid":       openid,
		"forceRefresh": forceRefresh,
	})
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiToken)

	cli := &http.Client{Timeout: 15 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return "", fmt.Errorf("第三方接口请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return "", fmt.Errorf("第三方 API Token 无效（401）")
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("第三方接口 HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("第三方接口返回非 JSON: %w", err)
	}

	code := extractCodeFromThirdparty(result)
	if code == "" {
		return "", fmt.Errorf("第三方接口未返回 code（响应: %s）", truncate(fmt.Sprintf("%v", result), 200))
	}
	return code, nil
}

func stripSuffix(s, suffix string) string {
	if strings.HasSuffix(s, suffix) {
		return s[:len(s)-len(suffix)]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
