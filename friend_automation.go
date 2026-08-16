package main

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/config"
	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/models"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// 极速务农(turbo)开启时：每轮最多处理的护主犬数量（用户指定 15，剩余下一轮继续）
const turboHelpRoundLimit = 15

// ============================================================
// 好友自动巡查引擎（对齐 Node core/worker.js unifiedScheduler：
// runStealTick 25–30s 偷菜 / runHelpTick 30–35s 帮忙+捣乱 / friend-orchestrator.js checkFriends）。
//
// 每日操作上限来自服务端 operation_limits（proto 已补解码，见 proto/plantpb.go OperationLimit）：
// 每次好友操作 reply 经 execFriendOp → updateOperationLimits 写入缓存，含 UTC+8 跨日重置。
// 偷(10004)/帮(10001-10003)/放虫草(10005-10006) 次数上限优先用服务端 day_times_lt，
// 放虫草再以本地计数兜底（对齐 Node localBadOperationCount）。黄金虫放置因 proto 无 social_items，跳过。
// ============================================================

// guardDogID 护主犬物品 ID（0x15FA5），对齐 Node friend-visit.js dogId=90021
const guardDogID = 90021

// 每轮帮忙农场数上限（对齐参考 GO maxHelpTargetsPerCycle）
const maxHelpTargetsPerCycle = 24
// 偷到后下一轮快扫间隔（对齐参考 GO rapidStealInterval）
const rapidStealInterval = time.Second

// badDailyLimit 每日放虫/草次数上限（对齐 Node friend-operation-limits.js BAD_DAILY_LIMIT=100，
// 作为服务端未回传 day_times_lt 时的兜底）
const badDailyLimit = 100

// badFailLimit 捣乱连续失败暂停阈值（对齐 Node BAD_FAILURE_LIMIT）
const badFailLimit = 3

// badPauseDuration 捣乱连续失败后暂停时长
const badPauseDuration = 12 * time.Hour

// 好友操作类型 ID（对齐 Node friend-operation-limits.js OP_NAMES）
const (
	opHelpWater  = 10001
	opHelpWeed   = 10002
	opHelpInsect = 10003
	opSteal      = 10004
	opPutBug     = 10005
	opPutWeed    = 10006
)

// ===== 服务端 operation_limits 缓存（对齐 Node friend-operation-limits.js operationLimits Map） =====
var (
	opLimitsMu    sync.Mutex
	opLimits      = map[int64]*opLimitState{}
	opLimitsKey   string // UTC+8 日期，跨日重置
	canGetHelpExp = true // 经验上限后仅帮护主犬（对齐 Node canGetHelpExp）

	// VisitorList 首次拉取标志（对齐 Node 首次加载用 VisitorList 做初始 friendList）
	firstFriendFetchMu   sync.Mutex
	firstFriendFetchDone = map[string]bool{}

	// 护主犬缓存全量刷新（对齐 Node bootstrapFriendDogInfoCacheIfNeeded，周期按用户要求改为 60min）
	lastFullDogInfoRefreshAt int64
	dogInfoBootstrapReadyAt  int64

	// expLimitCallback 经验上限跨日重置回调（对齐 Node ensureExpLimitCallback）
	onExpLimitReachedFn func()
	onExpLimitResetFn   func()
)

type opLimitState struct {
	DayTimes         int64
	DayTimesLimit    int64
	DayExpTimes      int64
	DayExpTimesLimit int64
}

// badDaily 本地兜底计数（对齐 Node localBadOperationCount）：服务端未回传时用作放虫草已用次数
var (
	badDailyMu  sync.Mutex
	badDailyCnt = map[string]int{}
	badDailyKey string
)

// ===== 防并发（对齐 Node isCheckingFriends） =====
var (
	checkingFriendsMu sync.Mutex
	isCheckingFriends bool
)

// ===== 捣乱连续失败暂停状态 =====
var (
	badFailMu          sync.Mutex
	badFailCount       int
	badPausedUntil     time.Time
	badPausedAccountID string
)

func checkOpLimitsDailyReset() {
	t := time.Now().UTC().Add(8 * time.Hour)
	key := t.Format("2006-01-02")
	opLimitsMu.Lock()
	prevKey := opLimitsKey
	if key != opLimitsKey {
		if prevKey != "" {
			opLimits = map[int64]*opLimitState{}
			canGetHelpExp = true
			// 调用跨日重置回调（对齐 Node onExpLimitResetCallback：清持久化经验上限标志）
			if onExpLimitResetFn != nil {
				onExpLimitResetFn()
			}
		}
		opLimitsKey = key
	}
	opLimitsMu.Unlock()
	badDailyMu.Lock()
	if key != badDailyKey {
		badDailyKey = key
		badDailyCnt = map[string]int{}
	}
	badDailyMu.Unlock()
}

// setOnExpLimitReachedCallback 注册经验上限回调（对齐 Node setOnExpLimitReachedCallback）
func setOnExpLimitReachedCallback(fn func()) {
	onExpLimitReachedFn = fn
}

// setOnExpLimitResetCallback 注册跨日重置回调（对齐 Node setOnExpLimitResetCallback）
func setOnExpLimitResetCallback(fn func()) {
	onExpLimitResetFn = fn
}

// updateOperationLimits 从每次农场/好友操作 reply 的 operation_limits 刷新缓存（对齐 Node updateOperationLimits）
func updateOperationLimits(limits []proto.OperationLimit) {
	if len(limits) == 0 {
		return
	}
	checkOpLimitsDailyReset()
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	for _, l := range limits {
		if l.ID <= 0 {
			continue
		}
		opLimits[l.ID] = &opLimitState{
			DayTimes:         l.DayTimes,
			DayTimesLimit:    l.DayTimesLimit,
			DayExpTimes:      l.DayExpTimes,
			DayExpTimesLimit: l.DayExpTimesLimit,
		}
	}
}

// canOperate 操作次数是否未达上限（对齐 Node canOperate(opId, fallbackLimit)）
func canOperate(opID, fallback int64) bool {
	checkOpLimitsDailyReset()
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	st, ok := opLimits[opID]
	if !ok {
		return true // 未知则允许
	}
	limit := st.DayTimesLimit
	if limit <= 0 {
		limit = fallback
	}
	if limit <= 0 {
		return true
	}
	return st.DayTimes < limit
}

func getOperationDayTimes(opID int64) int64 {
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	if st, ok := opLimits[opID]; ok {
		return st.DayTimes
	}
	return 0
}

// canGetExp 今日是否还能获得经验（对齐 Node canGetExp(opId)）
func canGetExp(opID int64) bool {
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	st, ok := opLimits[opID]
	if !ok {
		return true
	}
	if st.DayExpTimesLimit <= 0 {
		return true
	}
	return st.DayExpTimes < st.DayExpTimesLimit
}

func getCanGetHelpExp() bool {
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	return canGetHelpExp
}

func setCanGetHelpExp(v bool) {
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	canGetHelpExp = v
}

// autoDisableHelpByExpLimit 经验满后自动切换仅帮护主犬模式（对齐 Node autoDisableHelpByExpLimit）
func autoDisableHelpByExpLimit(accountID string) {
	if !getCanGetHelpExp() {
		return
	}
	setCanGetHelpExp(false)
	appendOpLog(accountID, "friend", "今日帮助经验已达上限，自动停止普通帮忙，仅帮助护主犬好友")
	// 持久化经验上限状态（对齐 Node onExpLimitReachedCallback → applyConfigSnapshot）
	if onExpLimitReachedFn != nil {
		onExpLimitReachedFn()
	}
}

// detectExpFull 帮忙后通过 exp 增量比对判定经验是否已满（对齐 Node helpWater 内的 detectExpFull：
// 每次 help RPC 后 sleep 200ms，比对 expBefore vs expAfter，若 expAfter <= expBefore 则判定经验满）
func detectExpFull(c *gw.Client, expBefore int64, accountID string) {
	expAfter := c.Exp()
	if expAfter <= expBefore {
		autoDisableHelpByExpLimit(accountID)
	}
}

// getBadRemainingTimes 今日放虫/草剩余次数（对齐 Node getBadRemainingTimes：BAD_DAILY_LIMIT - max(服务端,本地)）
func getBadRemainingTimes(accountID string) int64 {
	used := getOperationDayTimes(opPutBug) + getOperationDayTimes(opPutWeed)
	badDailyMu.Lock()
	local := int64(badDailyCnt[accountID])
	badDailyMu.Unlock()
	if local > used {
		used = local
	}
	return badDailyLimit - used
}

func incBadDaily(accountID string) {
	checkOpLimitsDailyReset()
	badDailyMu.Lock()
	defer badDailyMu.Unlock()
	badDailyCnt[accountID]++
}

// ===== 捣乱连续失败暂停（对齐 Node recordBadFailure + pauseFriendBadUntilTomorrow） =====

func isBadPaused(accountID string) bool {
	badFailMu.Lock()
	defer badFailMu.Unlock()
	if !badPausedUntil.IsZero() && time.Now().Before(badPausedUntil) {
		return true
	}
	if !badPausedUntil.IsZero() && time.Now().After(badPausedUntil) {
		badPausedUntil = time.Time{}
		badFailCount = 0
		badPausedAccountID = ""
	}
	return false
}

func resetBadFailureCount() {
	badFailMu.Lock()
	defer badFailMu.Unlock()
	badFailCount = 0
}

func recordBadFailure(accountID, reason string) bool {
	badFailMu.Lock()
	defer badFailMu.Unlock()
	badFailCount++
	fmt.Printf("[friend] 捣乱失败 %d/%d: %s\n", badFailCount, badFailLimit, reason)
	if badFailCount >= badFailLimit {
		badPausedUntil = time.Now().Add(badPauseDuration)
		badPausedAccountID = accountID
		appendOpLog(accountID, "friend", fmt.Sprintf("捣乱连续失败 %d 次，暂停至 %s", badFailLimit, badPausedUntil.Format("2006-01-02 15:04")))
		return true
	}
	return false
}

// isIgnorableBadFailureMessage 可忽略的捣乱失败消息（对齐 Node isIgnorableBadFailureMessage）
func isIgnorableBadFailureMessage(msg string) bool {
	ignorable := []string{"??", "No target", "?????", "1001046", "used up", "no target",
		"没有可捣乱土地", "捣乱失败或今日次数已用完", "今日次数已用完", "次数已用完",
		"已经放过", "来晚一步"}
	for _, kw := range ignorable {
		if len(kw) > 0 && len(msg) > 0 {
			for i := 0; i <= len(msg)-len(kw); i++ {
				if msg[i:i+len(kw)] == kw {
					return true
				}
			}
		}
	}
	return false
}

// checkFriends 好友巡查主流程（对齐 Node friend-orchestrator.js checkFriends：
// 偷 → 卖 → 帮 → 捣；护主犬信息随进入好友农场时刷新，见 doFriendOperation 内 cacheFriendDog）。
// bootstrapFriendDogInfoCacheIfNeeded 护主犬缓存全量刷新（对齐 Node bootstrapFriendDogInfoCacheIfNeeded）
// 缓存为空或距上次全量刷新超 60min → 遍历好友进农场重建护主犬缓存；失败下轮重试
func bootstrapFriendDogInfoCacheIfNeeded(c *gw.Client, accountID string, friends []*proto.GameFriend) {
	now := time.Now().Unix()
	// 首次启动延迟 2min（上号稳定后再拉，对齐 Node dogInfoBootstrapReadyAt）
	if dogInfoBootstrapReadyAt == 0 {
		dogInfoBootstrapReadyAt = now + 120
		return
	}
	if now < dogInfoBootstrapReadyAt {
		return
	}
	m, _ := readDogCache(accountID)
	_ = m
	// 【修复 2026-08-14】只按 60min 周期全量刷新护主犬缓存（对齐 Node 周期刷新意图：
	// 发现新护主犬 / 清除换狗、删好友后的伪护主犬）。
	// 原判断里 `!cacheEmpty` 在“该账号没有护主犬好友”时恒不满足 → 每轮 checkFriends 都全量
	// 遍历所有好友进农场拉狗信息并疯狂写日志/发 RPC（本账号即因此疯狂刷『护主犬缓存全量刷新』）。
	if now-lastFullDogInfoRefreshAt <= 3600 { // 60min（用户要求，Node 默认 30min）
		return
	}
	lastFullDogInfoRefreshAt = now
	appendOpLog(accountID, "friend", "护主犬缓存全量刷新开始")
	for _, f := range friends {
		if f == nil || f.GID <= 0 {
			continue
		}
		_, rep, err := enterFriendFarm(c, f.GID, 2, "")
		if err != nil {
			// 进入失败：清理该好友缓存（对齐 Node 失败下轮重试/清除伪护主犬）
			cacheFriendDog(f.GID, &proto.VisitEnterReply{})
			continue
		}
		cacheFriendDog(f.GID, rep)
		leaveFriendFarm(c, f.GID)
		time.Sleep(randomIntervalMs(100, 200))
	}
	appendOpLog(accountID, "friend", "护主犬缓存全量刷新完成")
}

func checkFriends(c *gw.Client, accountID string, cfg config.AccountConfig, onlySteal, onlyHelp bool) int64 {
	// 防并发（对齐 Node isCheckingFriends 互斥）
	checkingFriendsMu.Lock()
	if isCheckingFriends {
		checkingFriendsMu.Unlock()
		return 0
	}
	isCheckingFriends = true
	checkingFriendsMu.Unlock()
	defer func() {
		checkingFriendsMu.Lock()
		isCheckingFriends = false
		checkingFriendsMu.Unlock()
	}()

	// 整轮巡查超时兜底（对齐参考 GO 每轮 45s/90s ctx）：单轮卡死不拖死后续调度
	scanStart := time.Now()
	const scanDeadline = 90 * time.Second
	scanTimedOut := func() bool { return time.Since(scanStart) > scanDeadline }
	var stolenTotal, helpTotal int64

	// 静默时段检查（对齐 Node inFriendQuietHours）
	if inQuietHours(cfg) {
		return 0
	}

	// 从持久化配置恢复经验上限状态（对齐 Node checkFriends 开头恢复 friendHelpExpExhausted）
	if cfg.FriendHelpExpExhausted && getCanGetHelpExp() {
		setCanGetHelpExp(false)
		appendOpLog(accountID, "friend", "从配置恢复：经验已达上限状态，仅帮助护主犬好友")
	}

	acc := models.GetAccountByID(accountID)
	platform := "qq"
	if acc != nil && acc.Platform != "" {
		platform = acc.Platform
	}
	// 首次加载：额外调用 VisitorList RPC 合并初始好友列表（对齐 Node 首次用 VisitorList 做初始 friendList）
	firstFriendFetchMu.Lock()
	if !firstFriendFetchDone[accountID] {
		firstFriendFetchMu.Unlock()
		firstFriendFetchDone[accountID] = true
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		visitorReply, visitorErr := c.Request(ctx, "gamepb.interactpb.VisitorService", "GetInteractRecords",
			proto.EncodeInteractRecordsRequest(), 12*time.Second)
		cancel()
		if visitorErr == nil {
			records := proto.DecodeInteractRecordsReply(visitorReply.Body)
			seen := map[int64]bool{}
			for _, g := range cfg.KnownFriendGIDs {
				seen[g] = true
			}
			for _, rec := range records {
				if rec.VisitorGID > 0 && !seen[rec.VisitorGID] {
					seen[rec.VisitorGID] = true
					cfg.KnownFriendGIDs = append(cfg.KnownFriendGIDs, rec.VisitorGID)
				}
			}
			if len(cfg.KnownFriendGIDs) > len(seen) {
				_ = models.SetAccountConfig(accountID, cfg)
			}
		}
	} else {
		firstFriendFetchMu.Unlock()
	}

	friends, err := fetchAllFriends(c, platform, cfg.KnownFriendGIDs)
	if err != nil || len(friends) == 0 {
		return 0
	}

	// 护主犬缓存全量刷新（对齐 Node bootstrapFriendDogInfoCacheIfNeeded，周期按用户要求 60min）
	bootstrapFriendDogInfoCacheIfNeeded(c, accountID, friends)

	bl := readBlacklist(accountID)
	isBlacklisted := func(gid int64) (skipSteal, skipHelp bool) {
		if e, ok := bl[gid]; ok {
			return e.SkipSteal, e.SkipHelp
		}
		return false, false
	}
	hasGuardDog := func(gid int64) bool {
		if di, ok := getFriendDog(accountID, gid); ok {
			return di.DogID == guardDogID
		}
		return false
	}

	// ft 好友候选（gid/level/need 用于排序；need 越大越优先帮忙）
	type ft struct {
		gid   int64
		level int64
		need  int64
	}
	var stealTargets, helpTargets, badTargets []ft
	expLimitEnabled := cfg.Automation.FriendHelpExpLimit
	helpExpReached := expLimitEnabled && !getCanGetHelpExp()
	for _, f := range friends {
		if f == nil || f.GID <= 0 {
			continue
		}
		skSteal, skHelp := isBlacklisted(f.GID)
		p := f.Plant
		// 偷菜目标：steal_plant_num > 0，按等级降序（对齐 Node stealTargets level desc）
		if !onlyHelp && !skSteal && p != nil && p.StealPlantNum > 0 {
			stealTargets = append(stealTargets, ft{f.GID, f.Level, p.StealPlantNum})
		}
		// 帮忙目标：缺水/草/虫，need 降序、护主犬优先
		if !onlySteal && !skHelp && p != nil && (p.DryNum > 0 || p.WeedNum > 0 || p.InsectNum > 0) {
			isTurbo := cfg.Automation.FriendTurboMode
			// 极速务农：暂停一切巡查、只帮护主犬（用护主犬缓存判定，非护主犬不帮）
			if isTurbo {
				if !hasGuardDog(f.GID) {
					continue
				}
			} else if helpExpReached && !hasGuardDog(f.GID) {
				continue // 经验满限制：仅帮护主犬
			}
			need := p.DryNum + p.WeedNum + p.InsectNum
			if hasGuardDog(f.GID) {
				need += 1 << 40
			}
			helpTargets = append(helpTargets, ft{f.GID, f.Level, need})
		}
		// 捣乱目标：空农场（无作物或全 0），按等级降序（对齐 Node 空农场候选 level desc 前 20）
		if !onlySteal && !skHelp && !skSteal {
			empty := p == nil || (p.StealPlantNum == 0 && p.DryNum == 0 && p.WeedNum == 0 && p.InsectNum == 0)
			if empty {
				badTargets = append(badTargets, ft{f.GID, f.Level, 0})
			}
		}
	}

	sort.Slice(stealTargets, func(i, j int) bool { return stealTargets[i].level > stealTargets[j].level })
	sort.Slice(helpTargets, func(i, j int) bool { return helpTargets[i].need > helpTargets[j].need })
	sort.Slice(badTargets, func(i, j int) bool { return badTargets[i].level > badTargets[j].level })
	// 仅前 20 名空农场参与捣乱（对齐 Node 空农场候选 level desc 前 20）
	if len(badTargets) > 20 {
		badTargets = badTargets[:20]
	}

	// 1. 偷菜（对齐 Node 执行 steal → visitFriendForSteal）
	if !onlyHelp {
		for _, t := range stealTargets {
			if !canOperate(opSteal, 0) || scanTimedOut() {
				break // 偷菜次数已达服务端上限（未知则不限）或整轮超时
			}
			res := doFriendOperation(c, accountID, t.gid, "steal")
			if res != nil && res.EnterError != "" {
				continue // 进入失败（好友离线/不存在）跳过
			}
			if res != nil && res.Count > 0 {
				stolenTotal += res.Count
			}
		}
		// 偷完自动卖果实（对齐 Node sellAllFruits）
		if len(stealTargets) > 0 {
			autoSellAfterHarvest(accountID, c)
		}
	}

	// 2. 帮忙
	if !onlySteal {
		// 每轮帮忙农场数上限：极速务农 15，普通 24（对齐参考 GO maxHelpTargetsPerCycle），剩余下一轮继续
		limit := turboHelpRoundLimit
		if !cfg.Automation.FriendTurboMode {
			limit = maxHelpTargetsPerCycle
		}
		if len(helpTargets) > limit {
			helpTargets = helpTargets[:limit]
		}
		for _, t := range helpTargets {
			if scanTimedOut() {
				break // 整轮巡查超时
			}
			// 经验满判定可能在巡逻中途触发并翻转 canGetHelpExp=false。
			// 对非护主犬好友实时复核，否则开关触发后本轮剩余普通好友仍会被无差别帮助。
			if expLimitEnabled && !hasGuardDog(t.gid) && !getCanGetHelpExp() {
				continue
			}
			// 帮忙用 exp 增量比对检测经验上限（对齐 Node visitFriendForHelp 内 checkExpLimit）
			expBefore := c.Exp()
			res := doFriendOperation(c, accountID, t.gid, "help")
			if res != nil {
				helpTotal += res.Count
			}
			if res != nil && res.Count > 0 && expLimitEnabled && getCanGetHelpExp() {
				time.Sleep(200 * time.Millisecond)
				detectExpFull(c, expBefore, accountID)
			}
		}
	}

	// 2.5 黄金虫放置（极速务农：暂停一切巡查、涡轮不放金虫）
	if cfg.Automation.FriendGoldenBug && !cfg.Automation.FriendTurboMode {
		for _, t := range helpTargets {
			res := doFriendOperation(c, accountID, t.gid, "goldenbug")
			if res != nil && res.EnterError != "" {
				continue // 进入失败跳过
			}
			time.Sleep(randomIntervalMs(500, 1000))
		}
	}

	// 3. 捣乱（受每日上限约束，对齐 Node BAD_DAILY_LIMIT）
	if !onlySteal && cfg.Automation.FriendBad && !cfg.Automation.FriendTurboMode {
		if isBadPaused(accountID) {
			appendOpLog(accountID, "friend", "捣乱已暂停，等待恢复")
		} else {
			for _, t := range badTargets {
				if getBadRemainingTimes(accountID) <= 0 {
					appendOpLog(accountID, "friend", "今日捣乱次数已达上限")
					break
				}
				res := doFriendOperation(c, accountID, t.gid, "bad")
				if res != nil {
					if res.Count > 0 {
						incBadDaily(accountID)
						resetBadFailureCount()
					} else {
						msg := res.Message
						if !isIgnorableBadFailureMessage(msg) {
							if recordBadFailure(accountID, msg) {
								break
							}
						}
					}
				}
				time.Sleep(randomIntervalMs(100, 200))
			}
		}
	}

	// 4. 好友巡查后：对好友列表中已失效的好友自动移除（对齐 Node delBuddy 调用：检查未知 UID 自动删除）
	// Go 在进入好友农场失败时会触发 handleFriendEnterError 处理，此处补充对列表内 fetchDogInfo 返回 unknown 的好友调 DelBuddy
	for _, f := range friends {
		if f == nil || f.GID <= 0 {
			continue
		}
		// 如果好友的 plant 为 nil 且 level 为 0（可能是失效好友），尝试 DelBuddy
		if f.Plant == nil && f.Level <= 0 {
			// 静默删除：尝试 DelBuddy RPC，失败不报错
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := c.Request(ctx, "gamepb.friendpb.FriendService", "DelFriend",
				proto.EncodeDelFriendRequest(f.GID), 10*time.Second)
			cancel()
			if err == nil {
				appendOpLog(accountID, "friend", fmt.Sprintf("已自动移除失效好友 GID=%d", f.GID))
			}
		}
	}

	// 5. 自动同意好友申请（对齐 Node autoAcceptFriendApply：检查待处理申请并自动同意）
	autoAcceptFriendApply(c, accountID, cfg)

	// 本轮巡查汇总（对齐参考 GO stealSummary/helpSummary）
	appendOpLog(accountID, "friend", fmt.Sprintf("巡查汇总: 候选%d人 偷%d块 帮%d块", len(stealTargets)+len(helpTargets), stolenTotal, helpTotal))
	return stolenTotal
}

// autoAcceptFriendApply 自动同意好友申请（对齐 Node autoAcceptFriendApply + checkAndAcceptApplications）
func autoAcceptFriendApply(c *gw.Client, accountID string, cfg config.AccountConfig) {
	minLevel := cfg.AutoAcceptFriendMinLevel
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	rep, err := c.Request(ctx, "gamepb.friendpb.FriendService", "GetApplications",
		proto.EncodeGetApplicationsRequest(), 12*time.Second)
	if err != nil {
		return
	}
	apps := proto.DecodeGetApplicationsReply(rep.Body)
	if apps == nil || len(apps.Applications) == 0 {
		return
	}
	var acceptGIDs []int64
	for _, a := range apps.Applications {
		if a == nil || a.GID <= 0 {
			continue
		}
		if minLevel > 0 && a.Level < int64(minLevel) {
			fmt.Printf("[friend] 好友申请 %s 等级 %d < %d，跳过\n", a.Name, a.Level, minLevel)
			continue
		}
		acceptGIDs = append(acceptGIDs, a.GID)
	}
	if len(acceptGIDs) == 0 {
		return
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel2()
	arep, err := c.Request(ctx2, "gamepb.friendpb.FriendService", "AcceptFriends",
		proto.EncodeAcceptFriendsRequest(acceptGIDs), 12*time.Second)
	if err != nil {
		fmt.Printf("[friend] 自动同意好友申请失败: %v\n", err)
		return
	}
	accepted := proto.DecodeAcceptFriendsReply(arep.Body)
	if len(accepted) > 0 {
		var names []string
		for _, f := range accepted {
			name := f.Name
			if f.Remark != "" {
				name = f.Remark
			}
			names = append(names, name)
		}
		appendOpLog(accountID, "friend", fmt.Sprintf("自动同意好友申请 %d 人: %v", len(accepted), names))
		// 同步新好友 GID 到已知列表
		for _, f := range accepted {
			if f.GID > 0 {
				models.SetKnownFriendGids(accountID, append(cfg.KnownFriendGIDs, f.GID))
			}
		}
	}
}

// initExpLimitPersistence 注册经验上限持久化回调（对齐 Node ensureExpLimitCallback：
// 跨日重置清掉 persistent friendHelpExpExhausted，经验满时持久化应用 configSnapshot）
func initExpLimitPersistence() {
	setOnExpLimitReachedCallback(func() {
		accID := models.GetDefaultAccountID()
		if accID == "" {
			return
		}
		cfg := models.GetAccountConfig(accID)
		cfg.FriendHelpExpExhausted = true
		_ = models.SetAccountConfig(accID, cfg)
	})
	setOnExpLimitResetCallback(func() {
		accID := models.GetDefaultAccountID()
		if accID == "" {
			return
		}
		cfg := models.GetAccountConfig(accID)
		cfg.FriendHelpExpExhausted = false
		_ = models.SetAccountConfig(accID, cfg)
	})
}
