package main

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
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

// 极速务农护主犬分批的轮换起点（对齐 node friend-orchestrator turboRoundIndex：
// 避免每轮都取列表头导致后排护主犬永远轮不到）
// ft 好友候选（gid/level/need 用于排序；need 越大越优先帮忙）——包级定义，供护主犬轮换分片使用
type ft struct {
	gid   int64
	level int64
	need  int64
}

var turboRoundIndex int

// beijingMinutes 当前北京时间（UTC+8）的分钟数，用于极速务农定时段比较
func beijingMinutes() int {
	d := time.Now().UTC().Add(8 * time.Hour)
	return d.Hour()*60 + d.Minute()
}

// parseScheduleWindow 解析 "HH:mm-HH:mm" 时间段；非法/跨午夜返回 nil
func parseScheduleWindow(raw string) [2]int {
	m := regexp.MustCompile(`^(\d{1,2}):(\d{2})-(\d{1,2}):(\d{2})$`).FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return [2]int{}
	}
	s := mustInt(m[1])*60 + mustInt(m[2])
	e := mustInt(m[3])*60 + mustInt(m[4])
	if s >= e {
		return [2]int{}
	}

	return [2]int{s, e}
}

func mustInt(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

// computeEffectiveTurbo 极速务农当前是否「生效」（对齐 node friend-orchestrator computeEffectiveTurbo）：
// 总开关关 → 不生效；未启用定时 → 持续生效；启用定时 → 仅北京时间落在设定段 [start,end) 内生效，段外正常巡查
func computeEffectiveTurbo(cfg config.AccountConfig) bool {
	a := cfg.Automation
	if !a.FriendTurboMode {
		return false
	}
	if !a.FriendTurboScheduled {
		return true
	}
	win := parseScheduleWindow(a.FriendTurboScheduleTime)
	if win[0] == 0 && win[1] == 0 {
		return false
	}
	now := beijingMinutes()
	return now >= win[0] && now < win[1]
}

// rotateTargets 从轮换起点取 limit 个候选并推进回绕（对齐 node turboRoundIndex），保证全部护主犬都被覆盖
func rotateTargets(targets []ft, limit int) []ft {
	n := len(targets)
	if n <= limit {
		turboRoundIndex = 0
		return targets
	}
	start := turboRoundIndex % n
	chunk := make([]ft, 0, limit)
	for i := 0; i < limit; i++ {
		chunk = append(chunk, targets[(start+i)%n])
	}
	turboRoundIndex = (start + limit) % n
	return chunk
}

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
// 注意：Node 每账号独立进程（fork），模块级全局即单账号；Go 多账号共享进程内存，
// 故这里必须按 accountID 分桶，否则账号间互相污染（经验上限/操作次数跨账号生效）。
var (
	opLimitsMu    sync.Mutex
	opLimits      = map[string]map[int64]*opLimitState{} // accountID -> opID -> state
	opLimitsKey   string                                 // UTC+8 日期，跨日重置
	canGetHelpExp = map[string]bool{}                    // accountID -> 经验上限后仅帮护主犬（对齐 Node canGetHelpExp）

	// VisitorList 首次拉取标志（对齐 Node 首次加载用 VisitorList 做初始 friendList）
	firstFriendFetchMu   sync.Mutex
	firstFriendFetchDone = map[string]bool{}

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

// ===== 捣乱连续失败暂停状态（按账号隔离；对齐 Node recordBadFailure + pauseFriendBadUntilTomorrow） =====
var (
	badFailMu      sync.Mutex
	badFailCount   = map[string]int{}       // accountID -> 连续失败次数
	badPausedUntil = map[string]time.Time{} // accountID -> 暂停截止时间
)

func checkOpLimitsDailyReset() {
	t := time.Now().UTC().Add(8 * time.Hour)
	key := t.Format("2006-01-02")
	opLimitsMu.Lock()
	prevKey := opLimitsKey
	if key != opLimitsKey {
		if prevKey != "" {
			opLimits = map[string]map[int64]*opLimitState{}
			canGetHelpExp = map[string]bool{}
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
func updateOperationLimits(accountID string, limits []proto.OperationLimit) {
	if len(limits) == 0 {
		return
	}
	checkOpLimitsDailyReset()
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	accLimits, ok := opLimits[accountID]
	if !ok {
		accLimits = map[int64]*opLimitState{}
		opLimits[accountID] = accLimits
	}
	for _, l := range limits {
		if l.ID <= 0 {
			continue
		}
		accLimits[l.ID] = &opLimitState{
			DayTimes:         l.DayTimes,
			DayTimesLimit:    l.DayTimesLimit,
			DayExpTimes:      l.DayExpTimes,
			DayExpTimesLimit: l.DayExpTimesLimit,
		}
	}
}

// canOperate 操作次数是否未达上限（对齐 Node canOperate(opId, fallbackLimit)）
func canOperate(accountID string, opID, fallback int64) bool {
	checkOpLimitsDailyReset()
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	st, ok := opLimits[accountID][opID]
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

func getOperationDayTimes(accountID string, opID int64) int64 {
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	if st, ok := opLimits[accountID][opID]; ok {
		return st.DayTimes
	}
	return 0
}

// canGetExp 今日是否还能获得经验（对齐 Node canGetExp(opId)）
func canGetExp(accountID string, opID int64) bool {
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	st, ok := opLimits[accountID][opID]
	if !ok {
		return true
	}
	if st.DayExpTimesLimit <= 0 {
		return true
	}
	return st.DayExpTimes < st.DayExpTimesLimit
}

func getCanGetHelpExp(accountID string) bool {
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	v, ok := canGetHelpExp[accountID]
	if !ok {
		return true // 未触发过经验上限的账号默认可帮忙（对齐 Node canGetHelpExp 初始 true）
	}
	return v
}

func setCanGetHelpExp(accountID string, v bool) {
	opLimitsMu.Lock()
	defer opLimitsMu.Unlock()
	canGetHelpExp[accountID] = v
}

// autoDisableHelpByExpLimit 经验满后自动切换仅帮护主犬模式（对齐 Node autoDisableHelpByExpLimit）
func autoDisableHelpByExpLimit(accountID string) {
	if !getCanGetHelpExp(accountID) {
		return
	}
	setCanGetHelpExp(accountID, false)
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
	used := getOperationDayTimes(accountID, opPutBug) + getOperationDayTimes(accountID, opPutWeed)
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
	until, ok := badPausedUntil[accountID]
	if !ok || until.IsZero() {
		return false
	}
	if time.Now().Before(until) {
		return true
	}
	// 暂停到期：清除该账号状态（对齐 Node pauseFriendBadUntilTomorrow 次日恢复）
	delete(badPausedUntil, accountID)
	delete(badFailCount, accountID)
	return false
}

func resetBadFailureCount(accountID string) {
	badFailMu.Lock()
	defer badFailMu.Unlock()
	delete(badFailCount, accountID)
	delete(badPausedUntil, accountID)
}

func recordBadFailure(accountID, reason string) bool {
	badFailMu.Lock()
	defer badFailMu.Unlock()
	badFailCount[accountID]++
	fmt.Printf("[friend] 账号 %s 捣乱失败 %d/%d: %s\n", accountID, badFailCount[accountID], badFailLimit, reason)
	if badFailCount[accountID] >= badFailLimit {
		badPausedUntil[accountID] = time.Now().Add(badPauseDuration)
		appendOpLog(accountID, "friend", fmt.Sprintf("捣乱连续失败 %d 次，暂停至 %s", badFailLimit, badPausedUntil[accountID].Format("2006-01-02 15:04")))
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
// bootstrapFriendDogInfoCacheIfNeeded 护主犬缓存刷新（对齐 Node bootstrapFriendDogInfoCacheIfNeeded）
// 【2026-08-19】主动全量刷新已删除（对齐 Node 2026-08-15 决定）：周期遍历全部好友逐个
// enterFriendFarm 查狗（452 好友串行 ~90 秒、~900 个 RPC）会压垮 WS 连接导致掉线。
// 狗信息只靠日常偷菜/帮忙被动收集（doFriendOperation Enter 后 cacheFriendDog），
// 手动刷新走面板按钮 /api/friends/fetch-dog-info。保留函数名避免调用点改动。
func bootstrapFriendDogInfoCacheIfNeeded(c *gw.Client, accountID string, friends []*proto.GameFriend) {
	return
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
	var stolenTotal int64

	// 静默时段检查（对齐 Node inFriendQuietHours）
	if inQuietHours(cfg) {
		return 0
	}

	// 从持久化配置恢复经验上限状态（对齐 Node checkFriends 开头恢复 friendHelpExpExhausted）
	if cfg.FriendHelpExpExhausted && getCanGetHelpExp(accountID) {
		setCanGetHelpExp(accountID, false)
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

	// 好友名查表（供巡查明细日志显示「偷了谁/帮谁」，对齐 Node 逐好友日志）
	nameByGID := make(map[int64]string, len(friends))
	for _, f := range friends {
		if f != nil {
			nameByGID[f.GID] = f.Name
		}
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

	var stealTargets, helpTargets, badTargets []ft
	expLimitEnabled := cfg.Automation.FriendHelpExpLimit
	helpExpReached := expLimitEnabled && !getCanGetHelpExp(accountID)
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
			isTurbo := computeEffectiveTurbo(cfg)
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
			if !canOperate(accountID, opSteal, 0) || scanTimedOut() {
				break // 偷菜次数已达服务端上限（未知则不限）或整轮超时
			}
			res := doFriendOperation(c, accountID, t.gid, nameByGID[t.gid], "steal")
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
		turboEff := computeEffectiveTurbo(cfg)
		if !turboEff {
			limit = maxHelpTargetsPerCycle
		}
		if len(helpTargets) > limit {
			if turboEff {
				// 极速护主犬分批轮换起点，保证全部护主犬都被覆盖（对齐 node turboRoundIndex）
				helpTargets = rotateTargets(helpTargets, limit)
			} else {
				helpTargets = helpTargets[:limit]
			}
		}
		for _, t := range helpTargets {
			if scanTimedOut() {
				break // 整轮巡查超时
			}
			// 经验满判定可能在巡逻中途触发并翻转 canGetHelpExp=false。
			// 对非护主犬好友实时复核，否则开关触发后本轮剩余普通好友仍会被无差别帮助。
			if expLimitEnabled && !hasGuardDog(t.gid) && !getCanGetHelpExp(accountID) {
				continue
			}
			// 帮忙用 exp 增量比对检测经验上限（对齐 Node visitFriendForHelp 内 checkExpLimit）
			expBefore := c.Exp()
			res := doFriendOperation(c, accountID, t.gid, nameByGID[t.gid], "help")
			if res != nil && res.Count > 0 && expLimitEnabled && getCanGetHelpExp(accountID) {
				time.Sleep(200 * time.Millisecond)
				detectExpFull(c, expBefore, accountID)
			}
		}
	}

	// 2.5 黄金虫放置（极速务农：暂停一切巡查、涡轮不放金虫）
	if cfg.Automation.FriendGoldenBug && !computeEffectiveTurbo(cfg) {
		for _, t := range helpTargets {
			res := doFriendOperation(c, accountID, t.gid, nameByGID[t.gid], "goldenbug")
			if res != nil && res.EnterError != "" {
				continue // 进入失败跳过
			}
			time.Sleep(randomIntervalMs(500, 1000))
		}
	}

	// 3. 捣乱（受每日上限约束，对齐 Node BAD_DAILY_LIMIT）
	if !onlySteal && cfg.Automation.FriendBad && !computeEffectiveTurbo(cfg) {
		if isBadPaused(accountID) {
			appendOpLog(accountID, "friend", "捣乱已暂停，等待恢复")
		} else {
			for _, t := range badTargets {
				if getBadRemainingTimes(accountID) <= 0 {
					appendOpLog(accountID, "friend", "今日捣乱次数已达上限")
					break
				}
				res := doFriendOperation(c, accountID, t.gid, nameByGID[t.gid], "bad")
				if res != nil {
					if res.Count > 0 {
						incBadDaily(accountID)
						resetBadFailureCount(accountID)
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

	// 本轮巡查明细以逐好友日志呈现（偷/帮谁、菜名、数量），不再输出空洞的候选/汇总行
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
