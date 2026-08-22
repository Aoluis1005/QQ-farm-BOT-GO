package main

// 离线通知（MeoW）：账号掉线/自动重连成功时推送到手机 App。
// MeoW 免鉴权：GET https://api.chuckfang.com/{昵称}/{标题}/{内容}，中文需 URL 编码。
// 触发点：gwpool 掉线钩子（超时/读错误/被踢）+ 连接成功（恢复）。异步推送、失败静默、不影响主流程。

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/models"
)

const meowBase = "https://api.chuckfang.com/"

var (
	notifyMu      sync.Mutex
	lastOfflineAt = map[string]time.Time{} // 账号 -> 上次离线推送时间（限流防轰炸）
	offlineSince  = map[string]time.Time{} // 账号 -> 离线开始时间（恢复通知带掉线时长）
	offlineMarked = map[string]bool{}      // 账号当前是否处于"已推过离线、待恢复"状态
)

// cstNow 北京时间字符串（所有通知内容统一带时间）
func cstNow() string {
	return time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
}

func notifyEnabled() bool {
	return models.GetSystemConfig().OfflineNotifyEnabled
}

func notifyNick() string {
	return models.GetSystemConfig().OfflineNotifyNick
}

func notifyAccountName(accountID string) string {
	if acc := models.GetAccountByID(accountID); acc != nil && acc.Name != "" {
		return acc.Name
	}
	return "账号"
}

// notifyOffline 掉线通知：同一账号 cooldown 分钟内最多推一条（自动重连期间可能多次掉线）。
// 注意：离线状态标记（offlineMarked/offlineSince）不受限流影响——每次掉线都记录，
// 确保恢复通知一定发；限流只卡"离线推送"本身。
func notifyOffline(accountID, reason string) {
	sc := models.GetSystemConfig()
	if !sc.OfflineNotifyEnabled || sc.OfflineNotifyNick == "" {
		return
	}
	cooldown := sc.OfflineNotifyCooldownMin
	if cooldown <= 0 {
		cooldown = 10
	}
	notifyMu.Lock()
	// 1) 状态标记：每次掉线都记录，保证恢复通知必发
	offlineMarked[accountID] = true
	if _, ok := offlineSince[accountID]; !ok {
		offlineSince[accountID] = time.Now()
	}
	// 2) 推送限流：cooldown 内只发第一条离线
	if last, ok := lastOfflineAt[accountID]; ok && time.Since(last) < time.Duration(cooldown)*time.Minute {
		notifyMu.Unlock()
		return
	}
	lastOfflineAt[accountID] = time.Now()
	name := notifyAccountName(accountID)
	notifyMu.Unlock()
	go meowPush(sc.OfflineNotifyNick, "QQ农场离线提醒", fmt.Sprintf("[%s] %s 掉线：%s", cstNow(), name, reason))
}

// notifyRecovered 恢复通知：仅当此前推过离线才推（首次连接不会误推）
func notifyRecovered(accountID string) {
	sc := models.GetSystemConfig()
	if !sc.OfflineNotifyEnabled || sc.OfflineNotifyNick == "" {
		return
	}
	notifyMu.Lock()
	if !offlineMarked[accountID] {
		notifyMu.Unlock()
		return
	}
	offlineMarked[accountID] = false
	off := offlineSince[accountID]
	delete(offlineSince, accountID)
	name := notifyAccountName(accountID)
	notifyMu.Unlock()
	dur := ""
	if !off.IsZero() {
		if m := int(time.Since(off).Minutes()); m < 1 {
			dur = "（1 分钟内恢复）"
		} else {
			dur = fmt.Sprintf("（掉线 %d 分钟）", m)
		}
	}
	go meowPush(sc.OfflineNotifyNick, "QQ农场离线提醒", fmt.Sprintf("[%s] %s 已自动重连成功%s", cstNow(), name, dur))
}

// meowPush 实际推送：异步 goroutine 调用、5s 超时、失败静默
func meowPush(nick, title, content string) {
	u := meowBase + url.PathEscape(nick) + "/" + url.PathEscape(title) + "/" + url.PathEscape(content)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
}

// ============ 定时收益推送（每天北京时间指定时刻推一次今日收益） ============

var (
	reportMu         sync.Mutex
	lastReportTarget string // 上次已推送的"日期+时间"（YYYY-MM-DD HH:MM）
	// 防重键含时间：改 DailyReportTime 后 target 变化，当天新时间点会重新开放推送；
	// 跨日 day 变化同样失效。仅同一"日期+时间"重复检查时跳过（防一天多推）。
)

// startDailyReportScheduler 启动每日收益推送调度（main 启动时调用一次）
func startDailyReportScheduler() {
	go func() {
		for {
			time.Sleep(30 * time.Second)
			runDailyReportCheck()
		}
	}()
}

func runDailyReportCheck() {
	sc := models.GetSystemConfig()
	if !sc.DailyReportEnabled || sc.OfflineNotifyNick == "" {
		return
	}
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	target := now.Format("2006-01-02") + " " + sc.DailyReportTime
	reportMu.Lock()
	if lastReportTarget == target {
		reportMu.Unlock()
		return
	}
	reportMu.Unlock()
	if now.Format("15:04") != sc.DailyReportTime {
		return
	}
	reportMu.Lock()
	lastReportTarget = target
	reportMu.Unlock()
	// 组装日报：遍历所有账号，取今日金币收益 + 同气礼盒
	var lines []string
	for _, acc := range models.GetAccounts() {
		inc := getTodayIncome(acc.ID)
		gold := numOf(inc["totalGold"])
		gifts := numOf(inc["dogGifts"])
		name := acc.Name
		if name == "" {
			name = acc.ID
		}
		if gold <= 0 && gifts <= 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s 金币+%d 同气礼盒%d个", name, gold, gifts))
	}
	if len(lines) == 0 {
		return
	}
	content := fmt.Sprintf("[%s] 今日收益：%s", now.Format("15:04"), strings.Join(lines, "；"))
	go meowPush(sc.OfflineNotifyNick, "QQ农场收益日报", content)
}

// numOf 把接口值转成 int64（支持 int/int64/float64/string）
func numOf(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case string:
		if s, err := strconv.ParseInt(strings.ReplaceAll(n, ",", ""), 10, 64); err == nil {
			return s
		}
	}
	return 0
}
