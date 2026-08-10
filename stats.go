package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ============ 今日收益统计（对齐 Node services/stats.js） ============

type AccountStats struct {
	Date        string           `json:"date"`
	Operations  map[string]int64 `json:"operations"`
	TongQiGift  int64            `json:"tongQiGiftCount"`
	InitGold    int64            `json:"initGold"`
	InitExp     int64            `json:"initExp"`
	InitCoupon  int64            `json:"initCoupon"`
	LastGold    int64            `json:"lastGold"`
	LastExp     int64            `json:"lastExp"`
	GoldGained  int64            `json:"goldGained"`
	ExpGained   int64            `json:"expGained"`
	CouponGained int64           `json:"couponGained"`
	SavedAt     int64            `json:"savedAt"`
}

// newOperationMap 预初始化今日收益全部操作 key（对齐 Node operations 结构，缺 key 会导致 recordOperation 记不进）
func newOperationMap() map[string]int64 {
	return map[string]int64{
		"harvest": 0, "water": 0, "weed": 0, "bug": 0, "farming": 0,
		"fertilize": 0, "plant": 0, "steal": 0, "helpWater": 0, "helpWeed": 0,
		"helpBug": 0, "goldenBugClear": 0, "goldenBugPut": 0, "taskClaim": 0,
		"sell": 0, "upgrade": 0, "levelUp": 0,
	}
}

func newAccountStats() *AccountStats {
	return &AccountStats{Operations: newOperationMap()}
}

var (
	statsMu   sync.Mutex
	statsInit = map[string]bool{}
	statsData = map[string]*AccountStats{}
)

func todayKey() string {
	return time.Now().Format("2006-01-02")
}

func statsFilePath(accountID string) string {
	return filepath.Join(dataDir, "stats", accountID+".json")
}

// getAccountStats 加载并确保今日统计（跨天自动重置，对齐 Node checkAndResetDailyStats）
func getAccountStats(accountID string) *AccountStats {
	statsMu.Lock()
	defer statsMu.Unlock()
	if s, ok := statsData[accountID]; ok {
		if s.Date == todayKey() {
			return s
		}
		// 跨天重置
		s.Date = todayKey()
		s.Operations = newOperationMap()
		s.TongQiGift = 0
		s.GoldGained, s.ExpGained, s.CouponGained = 0, 0, 0
		return s
	}
	s := loadStatsFile(accountID)
	statsData[accountID] = s
	return s
}

func loadStatsFile(accountID string) *AccountStats {
	s := newAccountStats()
	b, err := os.ReadFile(statsFilePath(accountID))
	if err == nil {
		var loaded AccountStats
		if json.Unmarshal(b, &loaded) == nil && loaded.Date == todayKey() {
			s = &loaded
			if s.Operations == nil {
				s.Operations = newOperationMap()
			}
		}
	}
	s.Date = todayKey()
	return s
}

func saveStatsFile(accountID string, s *AccountStats) {
	dir := filepath.Join(dataDir, "stats")
	os.MkdirAll(dir, 0755)
	s.SavedAt = time.Now().UnixMilli()
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := statsFilePath(accountID) + ".tmp"
	if os.WriteFile(tmp, b, 0644) == nil {
		os.Rename(tmp, statsFilePath(accountID))
	}
}

// recordOperation 记录一次操作（对齐 Node recordOperation）
// recordGift 同气礼包（物品 101351 增量）累计
func recordGift(accountID string, delta int64) {
	s := getAccountStats(accountID)
	s.TongQiGift += delta
	saveStatsFile(accountID, s)
}

func recordOperation(accountID, opType string, count int64) {
	if opType == "" || count <= 0 {
		return
	}
	acc := getAccountStats(accountID)
	if _, ok := acc.Operations[opType]; ok {
		acc.Operations[opType] += count
	}
	saveStatsFile(accountID, acc)
}

// initStats 初始化账号统计（登录时记录初始金币/经验/点券）
func initStats(accountID string, gold, exp, coupon int64) {
	acc := getAccountStats(accountID)
	if !statsInit[accountID] {
		acc.InitGold, acc.InitExp, acc.InitCoupon = gold, exp, coupon
		acc.LastGold, acc.LastExp = gold, exp
		statsInit[accountID] = true
	}
	saveStatsFile(accountID, acc)
}

// updateStats 跟踪金币/经验增量（今日收益 totalGold 来源，对齐 Node updateStats）
func updateStats(accountID string, gold, exp int64) {
	acc := getAccountStats(accountID)
	if gold > acc.LastGold {
		acc.GoldGained += gold - acc.LastGold
	}
	if exp > acc.LastExp {
		acc.ExpGained += exp - acc.LastExp
	}
	acc.LastGold, acc.LastExp = gold, exp
	saveStatsFile(accountID, acc)
}

// getTodayIncome 今日收益数据（对齐前端 income 卡片字段）
func getTodayIncome(accountID string) map[string]interface{} {
	acc := getAccountStats(accountID)
	op := acc.Operations
	m := map[string]interface{}{
		"totalGold":   acc.GoldGained,
		"dogGifts":    acc.TongQiGift,
		"harvest":     op["harvest"],
		"steal":       op["steal"],
		"plant":       op["plant"],
		"fertilize":   op["fertilize"],
		"water":       op["water"],
		"weed":        op["weed"],
		"insecticide": op["bug"],
		"oneKeyFarm":  op["farming"],
		"helpWater":   op["helpWater"],
		"helpWeed":    op["helpWeed"],
		"helpInsect":  op["helpBug"],
		"clearGolden": op["goldenBugClear"],
		"putGolden":   op["goldenBugPut"],
		"task":        op["taskClaim"],
	}
	return m
}
