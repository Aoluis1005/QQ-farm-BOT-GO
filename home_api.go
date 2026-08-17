package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Aoluis1005/go-farm-bot/models"
)

func registerHomeAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/home/profile", handleHomeProfile)
	mux.HandleFunc("/api/home/income/today", handleHomeIncome)
	mux.HandleFunc("/api/home/patrol", handleHomePatrol)
	mux.HandleFunc("/api/home/logs", handleHomeLogs)
	mux.HandleFunc("/api/logs", handleLogsDelete)
}

func handleHomeProfile(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		accountID = "default"
	}

	acc := models.GetAccountByID(accountID)
	// 初始不填任何假数据（对齐 Node：未连接时返回真实账号状态 + connected:false）
	data := map[string]interface{}{
		"connected":   false,
		"name":        "",
		"uid":         "",
		"avatar":      "",
		"level":       int64(0),
		"gold":        int64(0),
		"coupons":     int64(0),
		"goldenBeans": int64(0),
		"exp":         int64(0),
		"expMax":      int64(0),
		"expPercent":  0,
	}
	// 未连接时也给出该账号的真实名/uid（来自账号库，非假数据）
	if acc != nil {
		if acc.Name != "" {
			data["name"] = acc.Name
		}
		if acc.UIN != "" {
			data["uid"] = acc.UIN
		} else {
			data["uid"] = acc.ID
		}
	}

	// 连接成功则用真实数据覆盖
	if c, err := clientPool.Get(accountID); err == nil && c.GID != 0 {
		data["connected"] = true
		if c.UserName() != "" {
			data["name"] = c.UserName()
		}
		data["uid"] = fmt.Sprintf("%d", c.GID)
		if c.Level() != 0 {
			data["level"] = c.Level()
		}
		data["gold"] = formatGold(c.Gold())
		data["exp"] = c.Exp()
		data["coupons"] = c.Coupon()
		data["goldenBeans"] = c.GoldBean()
		if c.Avatar() != "" {
			data["avatar"] = c.Avatar()
		}
		if c.Level() != 0 {
			data["expMax"] = expUpperFor(c.Level())
			data["expPercent"] = expPercentFor(c.Level(), c.Exp())
		}
	}

	writeJSON(w, map[string]interface{}{"ok": true, "data": data})
}

func handleHomeIncome(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	accountID = resolveAccountID(accountID)
	if accountID == "" {
		accs := models.GetAccounts()
		if len(accs) > 0 {
			accountID = accs[0].ID
		}
	}
	if accountID == "" {
		writeJSON(w, map[string]interface{}{"ok": true, "data": getTodayIncome("default")})
		return
	}
	// 连接成功则同步金币/经验增量
	if c, err := clientPool.Get(accountID); err == nil && c.GID != 0 {
		initStats(accountID, c.Gold(), c.Exp(), c.Coupon())
		updateStats(accountID, c.Gold(), c.Exp())
	}
	income := getTodayIncome(accountID)
	// 同气礼盒：Node 语义 = ItemNotify 推送(帮忙好友获得) → stats.TongQiGift 今日累计（跨天清零），
	// 非背包存量（income 卡片全是"今日"口径）。触发可靠性见 gw/client.go applyItemNotify + recordGift 日志。
	writeJSON(w, map[string]interface{}{"ok": true, "data": income})
}

func handleHomePatrol(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		accountID = "default"
	}

	// POST：保存巡查配置（单字段）
	if r.Method == http.MethodPost {
		var req struct {
			Key     string `json:"key"`
			Enabled *bool  `json:"enabled"`
			Min     *int   `json:"min"`
			Max     *int   `json:"max"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, 400, "bad json")
			return
		}
		autoKey := map[string]string{"steal": "friend_steal", "help": "friend_help", "farm": "farm"}[req.Key]
		if autoKey == "" {
			writeError(w, 400, "unknown key: "+req.Key)
			return
		}
		minV, maxV := 0, 0
		if req.Min != nil {
			minV = *req.Min
		}
		if req.Max != nil {
			maxV = *req.Max
		}
		if req.Enabled != nil {
			if err := models.SetAutomation(accountID, autoKey, *req.Enabled); err != nil {
				writeError(w, 500, err.Error())
				return
			}
		}
		if minV > 0 && maxV > 0 {
			if err := models.SetIntervals(accountID, req.Key, minV, maxV); err != nil {
				writeError(w, 500, err.Error())
				return
			}
		}
		writeJSON(w, map[string]interface{}{"ok": true, "key": req.Key})
		return
	}

	// GET：读取
	intervals := models.GetIntervals(accountID)
	automation := models.GetAutomation(accountID)

	writeJSON(w, map[string]interface{}{
		"ok": true,
		"data": map[string]interface{}{
			"steal": map[string]interface{}{
				"enabled": automation.FriendSteal,
				"min":     intervals.StealMin,
				"max":     intervals.StealMax,
			},
			"help": map[string]interface{}{
				"enabled": automation.FriendHelp,
				"min":     intervals.HelpMin,
				"max":     intervals.HelpMax,
			},
			"farm": map[string]interface{}{
				"enabled": automation.Farm,
				"min":     intervals.FarmMin,
				"max":     intervals.FarmMax,
			},
		},
	})
}

// DELETE /api/logs  清空操作日志（对齐 Node clearLogs：真正删除该账号日志文件，
// 否则前端清空后刷新页面又会从文件读回来）
func handleLogsDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		accountID = "default"
	}
	path := filepath.Join(dataDir, "logs", accountID+".log")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		writeError(w, 500, "清空日志失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "message": "日志已清空", "accountId": accountID})
}

func handleHomeLogs(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	lines := readOpLogs(accountID, 100)
	logs := make([]map[string]interface{}, 0, len(lines))
	for _, ln := range lines {
		if e := parseLogLine(ln); e != nil {
			logs = append(logs, e)
		}
	}
	// 日志按写入顺序为旧→新；整体翻转使 index0=最新，前端 v-for 自然把最新置顶、向下滚动
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": logs})
}

func formatGold(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := ""
	if v == 0 {
		s = "0"
	} else {
		for {
			s = fmt.Sprintf("%03d%s", v%1000, s)
			v /= 1000
			if v == 0 {
				break
			}
			s = "," + s
			if v < 1000 {
				s = fmt.Sprintf("%d", v) + s
				break
			}
		}
	}
	if neg {
		s = "-" + s
	}
	return s
}

// parseLogLine 解析日志行 "[date time] action detail" → 前端展示对象
// 字段对齐 Node status.ts normalizeLogEntry 消费的 /api/logs 条目：
// time/tag/msg/meta{event}（Node Dashboard.vue 渲染 [时间] [tag徽章] [event徽章] msg）
func parseLogLine(ln string) map[string]interface{} {
	i := strings.Index(ln, "] ")
	if i < 0 {
		return nil
	}
	t := strings.TrimPrefix(ln[:i], "[")
	rest := ln[i+2:]
	action := rest
	detail := ""
	if j := strings.Index(rest, " "); j >= 0 {
		action = rest[:j]
		detail = rest[j+1:]
	}
	return map[string]interface{}{
		"time": t, // "01-02 15:04:05"，前端取空格后部分显示
		"tag":  actionTagFor(action),
		"msg":  detail,
		"meta": map[string]interface{}{"event": action}, // 对齐 Node meta.event 徽章
	}
}

func actionTagFor(a string) string {
	m := map[string]string{"harvest": "收获", "fertilize": "催熟", "clear": "铲除", "upgrade": "升级", "work": "一键", "plant": "种植", "steal": "偷菜", "help": "帮忙", "full": "收获"}
	if v, ok := m[a]; ok {
		return v
	}
	return a
}

func actionColorFor(a string) string {
	switch a {
	case "harvest", "full":
		return "var(--good)"
	case "fertilize", "plant":
		return "var(--grow)"
	case "clear":
		return "var(--warn)"
	case "steal":
		return "var(--primary)"
	}
	return "var(--muted)"
}
