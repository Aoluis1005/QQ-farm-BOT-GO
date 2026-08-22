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
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

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
	Signals      map[string]interface{} `json:"signals"`
	Reasons      []string               `json:"reasons"`
	RecordCount  int                    `json:"recordCount"`
	SampleWindow int                    `json:"sampleWindowHours"`
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

// analyzeBotSamples 对样本池按 gid 分组并计算嫌疑画像（纯内存，无网络）
func analyzeBotSamples(accountID string) []botScanResult {
	botSampleMu.Lock()
	pool := botSamples[accountID]
	botSampleMu.Unlock()
	if len(pool) == 0 {
		return nil
	}

	// 按 gid 分组
	byGID := map[int64][]interactSample{}
	for _, s := range pool {
		byGID[s.GID] = append(byGID[s.GID], s)
	}

	results := make([]botScanResult, 0, len(byGID))
	for gid, samples := range byGID {
		if len(samples) < botMinSamples {
			continue // 样本不足，不判定
		}
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

	results := analyzeBotSamples(accountID)
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
