package main

// 好友"疑似外挂"检测（纯客户端行为分析）
//
// 数据源：InteractRecords（访客/互动记录，带服务器时间戳）。
// 设计原则（线程安全）：
//   - 本模块只在 HTTP handler goroutine 中执行；与巡查/偷菜/帮忙自动化（automationLoop）
//     无任何共享写状态。唯一的共享点是样本池 botSamples，用 botSampleMu 保护。
//   - 拉取记录走 c.Request（复用连接池，visitors handler 同款路径）：仅占用 1 个
//     normalSlot（上限 8），自动化串行占 1 个，剩余 6 个空闲，不会饿死自动化。
//   - 分析计算为纯内存操作（毫秒级），不占网络、不占槽位、不阻塞任何线程。
//   - 服务端时间戳用于样本去重与过期清理，不做任何写回，零副作用。

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/models"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ============================================================
// 样本池（内存累积，跨请求复用）
// ============================================================

type interactSample struct {
	GID    int64
	Action int32
	Time   int64 // 服务器时间（秒）
	Nick   string
	Avatar string
	Level  int32
}

const (
	botSampleWindowSec = 72 * 3600 // 样本保留窗口：72h
	botSampleMax       = 4000      // 每账号样本上限（超出按时间截断）
	botMinSamples      = 5         // 少于该条数不判定
)

var (
	botSampleMu sync.Mutex
	botSamples  = map[string][]interactSample{} // key: accountID
)

// mergeInteractSamples 把最新拉取的记录合并进样本池（按 gid+action+time 去重）
func mergeInteractSamples(accountID string, recs []*proto.InteractRecord) {
	botSampleMu.Lock()
	defer botSampleMu.Unlock()

	now := time.Now().Unix()
	pool := botSamples[accountID]

	// 过期清理（保留窗口内的）
	valid := pool[:0]
	for _, s := range pool {
		if now-s.Time <= botSampleWindowSec {
			valid = append(valid, s)
		}
	}
	pool = valid

	// 去重 merge
	seen := make(map[[3]int64]bool, len(pool)+len(recs))
	for _, s := range pool {
		seen[[3]int64{s.GID, int64(s.Action), s.Time}] = true
	}
	for _, rec := range recs {
		if rec.VisitorGID <= 0 || rec.ServerTime <= 0 {
			continue
		}
		k := [3]int64{rec.VisitorGID, int64(rec.ActionType), rec.ServerTime}
		if seen[k] {
			continue
		}
		seen[k] = true
		pool = append(pool, interactSample{
			GID:    rec.VisitorGID,
			Action: rec.ActionType,
			Time:   rec.ServerTime,
			Nick:   rec.Nick,
			Avatar: rec.AvatarURL,
			Level:  rec.Level,
		})
	}

	// 超上限：按时间降序截断（保留最新）
	if len(pool) > botSampleMax {
		sort.Slice(pool, func(i, j int) bool { return pool[i].Time > pool[j].Time })
		pool = pool[:botSampleMax]
	}
	botSamples[accountID] = pool
}

// ============================================================
// 分析
// ============================================================

type botScanResult struct {
	GID          int64                  `json:"gid"`
	Nick         string                 `json:"nick"`
	Avatar       string                 `json:"avatar"`
	Level        int32                  `json:"level"`
	Score        int                    `json:"score"`
	Risk         string                 `json:"risk"` // high|medium|low|clean
	IsGuardDog   bool                   `json:"isGuardDog"`
	Value        int                    `json:"value"`      // 好友帮价值 0-100
	ValueLevel   string                 `json:"valueLevel"` // high|normal|low|junk
	StealValue   int                    `json:"stealValue"` // 偷价值基础分（不含实时可偷数）
	ValueDetail  map[string]interface{} `json:"valueDetail"`
	Signals      map[string]interface{} `json:"signals"`
	Reasons      []string               `json:"reasons"`
	RecordCount  int                    `json:"recordCount"`
	SampleWindow int                    `json:"sampleWindowHours"`
}

// ============================================================
// 好友价值分（供自动化排序/前端展示；纯内存，无网络）
// 构成：护主犬 +38 + 帮回报(封顶32) + 活跃度(封顶20) − 偷我 − 捣乱 − 挂嫌疑
// ============================================================

type friendValue struct {
	GID        int64 `json:"gid"`
	Value      int   `json:"value"`
	Level      string `json:"level"`
	StealValue int   `json:"stealValue"` // 偷价值（快照，供前端展示）
	RiskScore  int   `json:"riskScore"`  // 挂嫌疑分（getStealValue 实时计算用）
	HelpCount  int   `json:"helpCount"`
	StealCount int   `json:"stealCount"`
	BadCount   int   `json:"badCount"`
	GuardDog   bool  `json:"guardDog"`
}

var (
	friendValueMu sync.Mutex
	friendValues  = map[string]map[int64]friendValue{} // accountID → gid → 价值
	// "我偷 TA" 的成功块数埋点（偷菜成功处 recordStealTo 写入，纯内存零网络）
	stealToMu    sync.Mutex
	stealTo      = map[string]map[int64]int64{} // accountID → gid → 累计偷取块数
	stealToDirty bool
	stealToPath  string // 持久化路径（启动时由 main 注入 dataDir）
)

// recordStealTo 记录"我对某好友偷菜成功块数"（friend_service.go 偷菜成功处调用）
func recordStealTo(accountID string, gid int64, n int64) {
	if gid <= 0 || n <= 0 {
		return
	}
	stealToMu.Lock()
	m := stealTo[accountID]
	if m == nil {
		m = map[int64]int64{}
	}
	m[gid] += n
	stealTo[accountID] = m
	stealToDirty = true
	stealToMu.Unlock()
	// 异步落盘（防抖：变更后最多 2 秒写一次），保证重启不丢
	go func() {
		time.Sleep(2 * time.Second)
		saveStealTo()
	}()
}

func getStealTo(accountID string, gid int64) int64 {
	stealToMu.Lock()
	defer stealToMu.Unlock()
	if m := stealTo[accountID]; m != nil {
		return m[gid]
	}
	return 0
}

// initStealToStore 注入持久化路径并在启动时加载历史埋点（main 启动调用）
func initStealToStore(path string) {
	stealToPath = path
	stealToMu.Lock()
	defer stealToMu.Unlock()
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &stealTo); err == nil {
			return
		}
		stealTo = map[string]map[int64]int64{}
	}
}

// saveStealTo 把埋点写盘（线程安全，失败静默）
func saveStealTo() {
	stealToMu.Lock()
	if !stealToDirty || stealToPath == "" {
		stealToMu.Unlock()
		return
	}
	data, err := json.Marshal(stealTo)
	stealToDirty = false
	stealToMu.Unlock()
	if err != nil {
		return
	}
	if err := os.WriteFile(stealToPath, data, 0644); err != nil {
		// 写失败时恢复 dirty 标记，下轮再试
		stealToMu.Lock()
		stealToDirty = true
		stealToMu.Unlock()
	}
}

// getFriendValue 自动化读价值分（无缓存时给默认中值，不阻塞）
func getFriendValue(accountID string, gid int64) int {
	friendValueMu.Lock()
	defer friendValueMu.Unlock()
	if m := friendValues[accountID]; m != nil {
		if fv, ok := m[gid]; ok {
			return fv.Value
		}
	}
	return 50
}

// computeFriendValue 计算单好友帮价值+偷价值并写入缓存（在 analyzeBotSamples 内顺带调用）
func computeFriendValue(accountID string, gid int64, guard bool, helpCnt, stealCnt, badCnt, activeHours, riskScore int) int {
	v := 0
	if guard {
		v += 38
	}
	v += minInt(helpCnt*3, 32)
	v += minInt(activeHours*2, 20)
	v -= stealCnt * 2
	v -= badCnt * 5
	v -= int(float64(riskScore) * 0.5)
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	level := valueLevel(v)
	// 偷价值（不含实时可偷数加成，排序时另加 plantNum×10）。
	// 只计正向产出（我偷TA）；不扣风险——偷菜是单向行为，TA 是否挂机不影响偷取产出
	sv := minInt(int(getStealTo(accountID, gid))*4, 40)
	if sv < 0 {
		sv = 0
	}
	if sv > 100 {
		sv = 100
	}
	friendValueMu.Lock()
	m := friendValues[accountID]
	if m == nil {
		m = map[int64]friendValue{}
	}
	m[gid] = friendValue{GID: gid, Value: v, Level: level, StealValue: sv, RiskScore: riskScore, HelpCount: helpCnt, StealCount: stealCnt, BadCount: badCnt, GuardDog: guard}
	friendValues[accountID] = m
	friendValueMu.Unlock()
	return v
}

// getStealValue 自动化读偷价值：实时计算（埋点 stealTo 变化立即反映，不依赖 bot-scan 缓存）
func getStealValue(accountID string, gid int64) int {
	sv := minInt(int(getStealTo(accountID, gid))*4, 40)
	if sv < 0 {
		sv = 0
	}
	if sv > 100 {
		sv = 100
	}
	return sv
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// valueLevel 价值分等级
func valueLevel(v int) string {
	switch {
	case v >= 70:
		return "high"
	case v >= 45:
		return "normal"
	case v >= 25:
		return "low"
	default:
		return "junk"
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// analyzeBotSamples 对样本池按 gid 分组计算嫌疑画像 + 价值分（纯内存，无网络）。
// friends 为好友列表全集合：样本池提供行为分，好友列表提供全集（无互动好友也评估静态价值）。
func analyzeBotSamples(accountID string, friends []*proto.GameFriend) []botScanResult {
	botSampleMu.Lock()
	pool := botSamples[accountID]
	botSampleMu.Unlock()

	// 按 gid 分组
	byGID := map[int64][]interactSample{}
	for _, s := range pool {
		byGID[s.GID] = append(byGID[s.GID], s)
	}

	results := make([]botScanResult, 0, len(byGID)+len(friends))
	seen := map[int64]bool{}
	for gid, samples := range byGID {
		if len(samples) < botMinSamples {
			seen[gid] = true // 样本不足不判定，但标记防好友全集分支重复
			continue
		}
		seen[gid] = true
		// 时间升序
		sort.Slice(samples, func(i, j int) bool { return samples[i].Time < samples[j].Time })

		last := samples[len(samples)-1]
		res := botScanResult{
			GID:          gid,
			Nick:         last.Nick,
			Avatar:       last.Avatar,
			Level:        last.Level,
			IsGuardDog:   isGuardDogFriend(accountID, gid),
			Signals:      map[string]interface{}{},
			RecordCount:  len(samples),
			SampleWindow: int((samples[len(samples)-1].Time - samples[0].Time) / 3600),
		}
		if res.Nick == "" {
			res.Nick = fmt.Sprintf("GID:%d", gid)
		}

		// ---- 信号计算 ----
		// 1) 间隔规律性：相邻操作时间间隔的均值/标准差
		var intervals []float64
		for i := 1; i < len(samples); i++ {
			d := float64(samples[i].Time - samples[i-1].Time)
			if d > 0 && d < 3600*6 { // 排除异常大间隔（对方可能多日未上线）
				intervals = append(intervals, d)
			}
		}
		var mean, std float64
		if len(intervals) > 0 {
			for _, d := range intervals {
				mean += d
			}
			mean /= float64(len(intervals))
			for _, d := range intervals {
				std += (d - mean) * (d - mean)
			}
			std = sqrtFloat(std / float64(len(intervals)))
		}
		// periodicity 0-1：间隔越稳定越接近 1（std/mean 越小越规律）
		var periodicity float64
		if mean > 0 {
			periodicity = clamp01(1 - (std/mean)/2)
		}

		// 2) 活跃时段 + 凌晨占比（按服务器时间的小时）
		hourCnt := map[int]int{}
		var night, total int
		for _, s := range samples {
			h := int((s.Time % 86400) / 3600)
			hourCnt[h]++
			total++
			if h >= 2 && h <= 5 {
				night++
			}
		}
		activeHours := len(hourCnt)
		nightRatio := 0.0
		if total > 0 {
			nightRatio = float64(night) / float64(total)
		}

		// 3) 日均操作频率
		spanDays := (float64(samples[len(samples)-1].Time-samples[0].Time)/86400 + 1)
		avgPerDay := float64(len(samples)) / spanDays

		// 4) 行为单一性：只帮不偷
		var stealCnt, helpCnt, badCnt int
		for _, s := range samples {
			switch s.Action {
			case 1:
				stealCnt++
			case 2:
				helpCnt++
			case 3:
				badCnt++
			}
		}
		helpOnly := stealCnt == 0 && helpCnt > 0

		// ---- 综合评分（0-100）----
		score := 0
		score += int(periodicity * 40)                              // 间隔规律性 40
		score += int(clamp01(float64(activeHours-12)/12) * 20)      // 活跃时长 20
		score += int(clamp01(nightRatio/0.3) * 20)                  // 凌晨占比 20
		score += int(clamp01(avgPerDay/50) * 20)                    // 日均频率 20
		if helpOnly {
			score += 8 // 行为单一性加分
		}
		if score > 100 {
			score = 100
		}

		// ---- 风险等级 ----
		risk := "clean"
		switch {
		case score >= 75:
			risk = "high"
		case score >= 50:
			risk = "medium"
		case score >= 30:
			risk = "low"
		}

		// ---- 好友价值分（顺带计算并写入缓存，供自动化排序/前端展示） ----
		value := computeFriendValue(accountID, gid, res.IsGuardDog, helpCnt, stealCnt, badCnt, activeHours, score)
		res.Value = value
		res.ValueLevel = valueLevel(value)
		res.StealValue = getStealValue(accountID, gid)
		res.ValueDetail = map[string]interface{}{
			"guardDog":    res.IsGuardDog,
			"help":        helpCnt,
			"steal":       stealCnt,
			"bad":         badCnt,
			"activeHours": activeHours,
			"riskScore":   score,
			"stealTo":     getStealTo(accountID, gid),
		}

		// ---- 命中信号文案 ----
		var reasons []string
		if periodicity >= 0.6 && mean >= 30 {
			reasons = append(reasons, fmt.Sprintf("间隔高度规律(均值%.0fs±%.0fs)", mean, std))
		}
		if activeHours >= 20 {
			reasons = append(reasons, fmt.Sprintf("近乎24h在线(%dh)", activeHours))
		}
		if nightRatio >= 0.25 {
			reasons = append(reasons, fmt.Sprintf("凌晨活跃占比%.0f%%", nightRatio*100))
		}
		if avgPerDay >= 50 {
			reasons = append(reasons, fmt.Sprintf("日均操作%.0f次", avgPerDay))
		}
		if helpOnly {
			reasons = append(reasons, "只帮不偷模式单一")
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "暂无明显异常")
		}

		res.Score = score
		res.Risk = risk
		res.Reasons = reasons
		res.Signals["intervalMeanSec"] = int(mean)
		res.Signals["intervalStdMs"] = int(std * 1000)
		res.Signals["periodicity"] = int(periodicity * 100)
		res.Signals["activeHours"] = activeHours
		res.Signals["nightRatio"] = int(nightRatio * 100)
		res.Signals["avgPerDay"] = int(avgPerDay)
		res.Signals["helpOnly"] = helpOnly
		res.Signals["steal"] = stealCnt
		res.Signals["help"] = helpCnt
		res.Signals["bad"] = badCnt

		results = append(results, res)
	}

	// 好友列表全集中无样本的好友：按静态价值评估（护主犬已知则加分，行为分缺失按 0）
	for _, f := range friends {
		if f == nil || f.GID <= 0 || seen[f.GID] {
			continue
		}
		seen[f.GID] = true
		guard := isGuardDogFriend(accountID, f.GID)
		value := computeFriendValue(accountID, f.GID, guard, 0, 0, 0, 0, 0)
		results = append(results, botScanResult{
			GID:        f.GID,
			Nick:       f.Name,
			Avatar:     f.AvatarURL,
			Level:      int32(f.Level),
			Score:      0,
			Risk:       "clean",
			IsGuardDog: guard,
			Value:      value,
			ValueLevel: valueLevel(value),
			StealValue: getStealValue(accountID, f.GID),
			ValueDetail: map[string]interface{}{
				"guardDog": guard, "help": 0, "steal": 0, "bad": 0, "activeHours": 0, "riskScore": 0, "stealTo": 0,
			},
			Signals:     map[string]interface{}{},
			Reasons:     []string{"暂无互动记录"},
			RecordCount: 0,
		})
	}

	// 按嫌疑分降序
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	return results
}

// isGuardDogFriend 判断是否为护主犬好友（dogId==90021，复用本地狗信息缓存）
func isGuardDogFriend(accountID string, gid int64) bool {
	d, ok := getFriendDog(accountID, gid)
	return ok && d.DogID == 90021
}

func sqrtFloat(v float64) float64 {
	return math.Sqrt(v)
}

// ============================================================
// HTTP 端点
// ============================================================

func registerFriendBotScanAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/friends/bot-scan", handleFriendBotScan)
}

func handleFriendBotScan(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}

	// 拉取最新交互记录并 merge 进样本池（多服务路由候选，取首个成功）
	var recs []*proto.InteractRecord
	var lastErr error
	for _, cand := range proto.InteractRecordCandidates {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		rep, rerr := c.Request(ctx, cand[0], cand[1], proto.EncodeInteractRecordsRequest(), 8*time.Second)
		cancel()
		if rerr != nil {
			lastErr = rerr
			continue
		}
		recs = proto.DecodeInteractRecordsReply(rep.Body)
		if len(recs) > 0 {
			break
		}
	}
	if recs != nil {
		mergeInteractSamples(accountID, recs)
	}

	// 好友列表全集合（带 60s 展示缓存，命中零 RPC；未命中一次拉取）
	var friends []*proto.GameFriend
	acc := models.GetAccountByID(accountID)
	platform := "qq"
	if acc != nil && acc.Platform != "" {
		platform = acc.Platform
	}
	cfg := models.GetAccountConfig(accountID)
	if fl, ferr := getAllFriendsCached(c, accountID, platform, cfg.KnownFriendGIDs, false); ferr == nil {
		friends = fl
	}

	results := analyzeBotSamples(accountID, friends)
	hint := ""
	if len(results) == 0 {
		if lastErr != nil {
			hint = "拉取访客记录失败: " + lastErr.Error()
		} else if len(recs) == 0 {
			hint = "暂无访客记录，检测需要积累样本（建议挂机一段时间后再看）"
		} else {
			hint = "样本不足（每人少于5条），暂不判定"
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": results, "errorHint": hint})
}
