package main

import (
	"sort"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/config"
	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/models"
)

// ============================================================
// 好友自动巡查引擎（对齐 Node core/worker.js unifiedScheduler：
// runStealTick 25–30s 偷菜 / runHelpTick 30–35s 帮忙+捣乱 / friend-orchestrator.js checkFriends）。
//
// 说明：Go 侧 proto 不回传 operation_limits，故每日捣乱上限用本地计数（UTC+8 跨日重置）
// 近似 Node friend-operation-limits.js BAD_DAILY_LIMIT=100；经验上限在开启 FriendHelpExpLimit
// 时退化为"仅帮护主犬"。黄金虫放置因 proto 无 social_items 字段，按既定规则跳过。
// ============================================================

// guardDogID 护主犬物品 ID（0x15FA5），对齐 Node friend-visit.js dogId=90021
const guardDogID = 90021

// badDailyLimit 每日放虫/草次数上限（对齐 Node friend-operation-limits.js BAD_DAILY_LIMIT=100）
const badDailyLimit = 100

var (
	badDailyMu  sync.Mutex
	badDailyCnt = map[string]int{} // accountID -> 今日放虫/草次数
	badDailyKey string            // UTC+8 日期，用于跨日重置
)

func resetBadDailyIfNeeded() {
	t := time.Now().UTC().Add(8 * time.Hour) // UTC+8 跨日重置
	key := t.Format("2006-01-02")
	badDailyMu.Lock()
	defer badDailyMu.Unlock()
	if key != badDailyKey {
		badDailyKey = key
		badDailyCnt = map[string]int{}
	}
}

func badDailyCount(accountID string) int {
	resetBadDailyIfNeeded()
	badDailyMu.Lock()
	defer badDailyMu.Unlock()
	return badDailyCnt[accountID]
}

func incBadDaily(accountID string) {
	resetBadDailyIfNeeded()
	badDailyMu.Lock()
	defer badDailyMu.Unlock()
	badDailyCnt[accountID]++
}

// friendStealLoop 偷菜循环（对齐 Node unifiedScheduler runStealTick：25–30s）
func friendStealLoop(accountID string, stop chan struct{}) {
	for {
		cfg := models.GetAccountConfig(accountID)
		if cfg.Automation.Friend && cfg.Automation.FriendSteal {
			if c, err := clientPool.Get(accountID); err == nil && c != nil {
				checkFriends(c, accountID, cfg, true, false)
			}
		}
		cfg = models.GetAccountConfig(accountID)
		select {
		case <-stop:
			return
		case <-time.After(randomIntervalMs(25*1000, 30*1000)):
		}
	}
}

// friendHelpLoop 帮忙/捣乱循环（对齐 Node unifiedScheduler runHelpTick：30–35s）
func friendHelpLoop(accountID string, stop chan struct{}) {
	for {
		cfg := models.GetAccountConfig(accountID)
		if cfg.Automation.Friend && cfg.Automation.FriendHelp {
			if c, err := clientPool.Get(accountID); err == nil && c != nil {
				checkFriends(c, accountID, cfg, false, true)
			}
		}
		cfg = models.GetAccountConfig(accountID)
		select {
		case <-stop:
			return
		case <-time.After(randomIntervalMs(30*1000, 35*1000)):
		}
	}
}

// checkFriends 好友巡查主流程（对齐 Node friend-orchestrator.js checkFriends：
// 偷 → 卖 → 帮 → 捣；护主犬信息随进入好友农场时刷新，见 doFriendOperation 内 cacheFriendDog）。
func checkFriends(c *gw.Client, accountID string, cfg config.AccountConfig, onlySteal, onlyHelp bool) {
	acc := models.GetAccountByID(accountID)
	platform := "qq"
	if acc != nil && acc.Platform != "" {
		platform = acc.Platform
	}
	friends, err := fetchAllFriends(c, platform, cfg.KnownFriendGIDs)
	if err != nil || len(friends) == 0 {
		return
	}

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
		// 帮忙目标：缺水/草/虫，need 降序、护主犬优先（对齐 Node helpTargets need desc + guard dog 优先）
		if !onlySteal && !skHelp && p != nil && (p.DryNum > 0 || p.WeedNum > 0 || p.InsectNum > 0) {
			if cfg.Automation.FriendHelpExpLimit && !hasGuardDog(f.GID) {
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
			res := doFriendOperation(c, accountID, t.gid, "steal")
			if res != nil && res.EnterError != "" {
				continue // 进入失败（好友离线/不存在）跳过
			}
			time.Sleep(randomIntervalMs(800, 1500))
		}
		// 偷完自动卖果实（对齐 Node sellAllFruits）
		if len(stealTargets) > 0 {
			autoSellAfterHarvest(accountID, c)
		}
	}

	// 2. 帮忙（对齐 Node 执行 help → visitFriendForHelp，单次进入内浇水/除草/除虫）
	if !onlySteal {
		for _, t := range helpTargets {
			doFriendOperation(c, accountID, t.gid, "help")
			time.Sleep(randomIntervalMs(800, 1500))
		}
	}

	// 3. 捣乱（受每日上限约束，对齐 Node BAD_DAILY_LIMIT）
	if !onlySteal && cfg.Automation.FriendBad {
		for _, t := range badTargets {
			if badDailyCount(accountID) >= badDailyLimit {
				appendOpLog(accountID, "friend", "今日捣乱次数已达上限")
				break
			}
			res := doFriendOperation(c, accountID, t.gid, "bad")
			if res != nil && res.Count > 0 {
				incBadDaily(accountID)
			}
			time.Sleep(randomIntervalMs(800, 1500))
		}
	}
}
