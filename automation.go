package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/config"
	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/models"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ============================================================
// 自动化引擎（对齐 Node core/worker.js unifiedScheduler + services/farming-orchestrator.js
// + services/planting-service.js + services/farm-fertilizer.js + services/farm-scheduler.js）
//
// 设计：每个账号一个 runner（含若干 goroutine），按 config.AutomationConfig 决定开哪些循环。
// 目前接入：农场巡田（水/草/虫/收/种/施肥/升级）、自动卖果实、自动买化肥。
// 好友巡查（偷/帮/捣）、护主犬刷新、每日任务等在后续批次接入。
// ============================================================

var (
	automationMu      sync.Mutex
	automationRunners = map[string]*automationRunner{}
)

type automationRunner struct {
	stop chan struct{}
}

// startAutomationForAccount 为账号启动所有自动化 goroutine（已存在则先停后起）
func startAutomationForAccount(accountID string) {
	automationMu.Lock()
	if r, ok := automationRunners[accountID]; ok {
		close(r.stop)
		delete(automationRunners, accountID)
	}
	stop := make(chan struct{})
	automationRunners[accountID] = &automationRunner{stop: stop}
	automationMu.Unlock()

	go farmAutomationLoop(accountID, stop)
	go fertilizeBuyLoop(accountID, stop)
	go friendStealLoop(accountID, stop)
	go friendHelpLoop(accountID, stop)
	log.Printf("[automation] 账号 %s 自动化已启动", accountID)
}

// stopAutomationForAccount 停止账号的全部自动化 goroutine
func stopAutomationForAccount(accountID string) {
	automationMu.Lock()
	defer automationMu.Unlock()
	if r, ok := automationRunners[accountID]; ok {
		close(r.stop)
		delete(automationRunners, accountID)
		log.Printf("[automation] 账号 %s 自动化已停止", accountID)
	}
}

// startAllAutomation 为所有已配置账号启动自动化（main 启动时调用）
func startAllAutomation() {
	for _, acc := range models.GetAccounts() {
		startAutomationForAccount(acc.ID)
	}
}

// ── 间隔工具（对齐 Node randomIntervalMs / applyIntervalsToRuntime） ──

func randomIntervalMs(minMs, maxMs int) time.Duration {
	if maxMs <= minMs {
		if minMs < 0 {
			minMs = 0
		}
		return time.Duration(minMs) * time.Millisecond
	}
	return time.Duration(minMs+rand.Intn(maxMs-minMs+1)) * time.Millisecond
}

// farmInterval 农场巡查间隔（秒→毫秒），默认 2–5s
func farmInterval(iv config.IntervalsConfig) time.Duration {
	minS, maxS := iv.FarmMin, iv.FarmMax
	if minS <= 0 {
		minS = iv.Farm
	}
	if minS <= 0 {
		minS = 2
	}
	if maxS <= 0 {
		maxS = minS
	}
	if maxS < minS {
		maxS = minS
	}
	return randomIntervalMs(minS*1000, maxS*1000)
}

// ── 农场巡田主循环（对齐 Node unifiedScheduler.runFarmTick → checkFarm） ──

func farmAutomationLoop(accountID string, stop chan struct{}) {
	for {
		cfg := models.GetAccountConfig(accountID)
		if cfg.Automation.Farm {
			if c, err := clientPool.Get(accountID); err == nil && c != nil {
				runFarmOnce(accountID, c, cfg)
			}
		}
		cfg = models.GetAccountConfig(accountID)
		iv := farmInterval(cfg.Intervals)
		select {
		case <-stop:
			return
		case <-time.After(iv):
		}
	}
}

// runFarmOnce 单次巡田（对齐 Node farming-orchestrator.js runFarmOperation('all') 顺序）
func runFarmOnce(accountID string, c *gw.Client, cfg config.AccountConfig) {
	ctx := context.Background()
	rep, err := c.Request(ctx, plantService, "AllLands", proto.EncodeAllLandsRequest(0), 15*time.Second)
	if err != nil {
		return
	}
	lands := proto.DecodeAllLandsReply(rep.Body).Lands
	now := time.Now().Unix()
	a := analyzeFarmLands(lands, now)

	// 1. 一键务农（水/草/虫），对齐 Node runFarmOperation 步骤1
	var farmingIDs []int64
	farmingIDs = append(farmingIDs, a.needWater...)
	if !cfg.Automation.SkipOwnWeedBug {
		farmingIDs = append(farmingIDs, a.needWeed...)
		farmingIDs = append(farmingIDs, a.needBug...)
	}
	// GoldenBugClear：黄金虫本质也是草/虫，已并入 needWeed/needBug 的务农调用
	farmingIDs = dedupeInt64(farmingIDs)
	if len(farmingIDs) > 0 {
		if err := execFarmOp(c, "Farming", proto.EncodeFarmingRequest(farmingIDs, c.GID)); err == nil {
			recordOperation(accountID, "farming", int64(len(farmingIDs)))
			appendOpLog(accountID, "farm", fmt.Sprintf("一键务农 %d 块地", len(farmingIDs)))
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 2. 收获（对齐 Node 步骤2，harvest 后触发自动卖）
	if len(a.harvestable) > 0 {
		if err := execFarmOp(c, "Harvest", proto.EncodeHarvestRequest(a.harvestable, c.GID, false)); err == nil {
			recordOperation(accountID, "harvest", int64(len(a.harvestable)))
			appendOpLog(accountID, "farm", fmt.Sprintf("收获 %d 块地", len(a.harvestable)))
			if cfg.Automation.Sell {
				autoSellAfterHarvest(accountID, c)
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 2.5 重新拉取农场地块，刷新枯死/空地/刚收获变空的地（对齐 Node resolveRemovableHarvestedLands：
	// 收获后 mature 地块变空，需并入种植目标；dead/empty 也在本次快照内重算）
	if len(a.harvestable) > 0 || len(a.dead) > 0 || len(a.empty) > 0 {
		if rep2, e2 := c.Request(ctx, plantService, "AllLands", proto.EncodeAllLandsRequest(0), 15*time.Second); e2 == nil {
			lands = proto.DecodeAllLandsReply(rep2.Body).Lands
			a = analyzeFarmLands(lands, time.Now().Unix())
		}
	}

	// 3. 种植（枯死 + 空地），对齐 Node 步骤3 autoPlantEmptyLands
	plantTargets := append([]int64{}, a.dead...)
	plantTargets = append(plantTargets, a.empty...)
	plantTargets = dedupeInt64(plantTargets)
	if len(plantTargets) > 0 {
		autoPlantLands(accountID, c, cfg, lands, plantTargets)
		time.Sleep(300 * time.Millisecond)
	}

	// 4. 多季补肥（对齐 Node 步骤4），reason=multi_season
	if cfg.Automation.FertilizerMultiSeason && cfg.Automation.Fertilizer != "final_normal" && len(a.growing) > 0 {
		runFertilizerByConfig(accountID, c, cfg, a.growing, "multi_season", false)
	}

	// 5. 解锁 + 升级（对齐 Node 步骤5，先解锁后升级）
	if cfg.Automation.LandUpgrade {
		for _, id := range a.couldUnlock {
			if err := execFarmOp(c, "UnlockLand", proto.EncodeUnlockLandRequest(id, false)); err == nil {
				appendOpLog(accountID, "farm", fmt.Sprintf("解锁土地 %d", id))
			}
			time.Sleep(200 * time.Millisecond)
		}
		for _, id := range a.couldUpgrade {
			if err := execFarmOp(c, "UpgradeLand", proto.EncodeUpgradeLandRequest(id)); err == nil {
				recordOperation(accountID, "upgrade", 1)
				appendOpLog(accountID, "farm", fmt.Sprintf("升级土地 %d", id))
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// 6. 智能施肥（farm 循环内，对齐 Node 步骤6：runFertilizerByConfig([], {skipNormal:true})）
	mode := cfg.Automation.Fertilizer
	if mode == "smart" || mode == "smart_only" || mode == "smart_normal" || mode == "final_normal" || mode == "final_organic" {
		runFertilizerByConfig(accountID, c, cfg, nil, "", true)
	}
}

// farmAnalysis 地块分类（对齐 Node farm-land-analyzer.js analyzeLands），只处理已解锁且非从属地块
type farmAnalysis struct {
	needWater     []int64
	needWeed      []int64
	needBug       []int64
	harvestable   []int64
	dead          []int64
	empty         []int64
	growing       []int64
	couldUnlock   []int64
	couldUpgrade  []int64
}

func analyzeFarmLands(lands []*proto.LandInfo, now int64) farmAnalysis {
	var a farmAnalysis
	for _, l := range lands {
		if l == nil || l.ID <= 0 {
			continue
		}
		if !l.Unlocked {
			if l.CouldUnlock {
				a.couldUnlock = append(a.couldUnlock, l.ID)
			}
			continue
		}
		// 从属土地（2x2 的 slave）由 master 统一处理，跳过单独操作
		if l.MasterLandID > 0 {
			continue
		}
		if l.Plant == nil || len(l.Plant.Phases) == 0 {
			a.empty = append(a.empty, l.ID)
			continue
		}
		ph := currentPhase(l.Plant.Phases, now)
		if ph == nil {
			a.empty = append(a.empty, l.ID)
			continue
		}
		switch ph.Phase {
		case proto.PhaseDead:
			a.dead = append(a.dead, l.ID)
			continue
		case proto.PhaseMature:
			a.harvestable = append(a.harvestable, l.ID)
			continue
		}
		// growing
		a.growing = append(a.growing, l.ID)
		if l.Plant.DryNum > 0 || (ph.DryTime > 0 && ph.DryTime <= now) {
			a.needWater = append(a.needWater, l.ID)
		}
		if len(l.Plant.WeedOwners) > 0 || (ph.WeedsTime > 0 && ph.WeedsTime <= now) {
			a.needWeed = append(a.needWeed, l.ID)
		}
		if len(l.Plant.InsectOwners) > 0 || (ph.InsectTime > 0 && ph.InsectTime <= now) {
			a.needBug = append(a.needBug, l.ID)
		}
	}
	return a
}

func dedupeInt64(in []int64) []int64 {
	if len(in) == 0 {
		return in
	}
	seen := map[int64]bool{}
	out := make([]int64, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// ── 种植（对齐 Node planting-service.js autoPlantEmptyLands / plantFromShop） ──

// autoPlantLands 在给定空地/枯死地上种植（targetLandIDs 为 master/standalone 地块）
func autoPlantLands(accountID string, c *gw.Client, cfg config.AccountConfig, lands []*proto.LandInfo, targetLandIDs []int64) {
	landByID := map[int64]*proto.LandInfo{}
	for _, l := range lands {
		if l != nil {
			landByID[l.ID] = l
		}
	}
	type group struct {
		ids []int64
	}
	var groups []group
	for _, id := range targetLandIDs {
		l := landByID[id]
		if l == nil {
			continue
		}
		g := group{ids: []int64{id}}
		if l.LandSize > 1 && len(l.SlaveLandIDs) > 0 {
			g.ids = append(g.ids, l.SlaveLandIDs...)
		}
		groups = append(groups, g)
	}
	if len(groups) == 0 {
		return
	}

	strategy := cfg.PlantingStrategy
	if strategy == "" {
		strategy = "level"
	}

	for _, g := range groups {
		// 枯死作物需先铲除再种（对齐 Node autoPlantEmptyLands：先 removePlant(dead) 再种植）
		if l := landByID[g.ids[0]]; l != nil && l.Plant != nil && len(l.Plant.Phases) > 0 {
			if ph := currentPhase(l.Plant.Phases, time.Now().Unix()); ph != nil && ph.Phase == proto.PhaseDead {
				_ = execFarmOp(c, "RemovePlant", proto.EncodeRemovePlantRequest(g.ids))
				time.Sleep(200 * time.Millisecond)
			}
		}
		seedID, goodsID, price, err := pickSeedForPlanting(accountID, c, cfg, strategy, len(g.ids))
		if err != nil || seedID <= 0 {
			appendOpLog(accountID, "farm", "种植跳过：无可用种子 ("+err.Error()+")")
			continue
		}
		if err := ensureSeedOwned(c, seedID, goodsID, price, len(g.ids)); err != nil {
			appendOpLog(accountID, "farm", fmt.Sprintf("购买种子 %d 失败: %v", seedID, err))
			continue
		}
		if err := execFarmOp(c, "Plant", proto.EncodePlantRequest(seedID, g.ids)); err != nil {
			appendOpLog(accountID, "farm", fmt.Sprintf("种植 %d 失败: %v", seedID, err))
			continue
		}
		recordOperation(accountID, "plant", int64(len(g.ids)))
		appendOpLog(accountID, "farm", fmt.Sprintf("种植种子 %d → %d 块地", seedID, len(g.ids)))
		delay := time.Duration(cfg.PlantDelaySeconds) * time.Second
		if delay <= 0 {
			delay = 2 * time.Second
		}
		time.Sleep(delay + 200*time.Millisecond)
	}
}

// plantOnLands 在指定地块种植指定种子（对齐 Node planting-service.js plantSeeds：
// 逐块传 [landId]，2x2 补全从属地，枯死地块先铲除，确保种子库存后种植）。
// 用于前端手动种植 / /api/farm/action action=plant。
func plantOnLands(accountID string, c *gw.Client, seedID int64, landIDs []int64) (int, error) {
	if len(landIDs) == 0 || seedID <= 0 {
		return 0, fmt.Errorf("invalid params")
	}
	rep, err := c.Request(context.Background(), plantService, "AllLands", proto.EncodeAllLandsRequest(0), 15*time.Second)
	if err != nil {
		return 0, err
	}
	lands := proto.DecodeAllLandsReply(rep.Body).Lands
	landByID := map[int64]*proto.LandInfo{}
	for _, l := range lands {
		if l != nil {
			landByID[l.ID] = l
		}
	}
	var fullIDs []int64
	for _, id := range landIDs {
		l := landByID[id]
		fullIDs = append(fullIDs, id)
		if l != nil && l.LandSize > 1 && len(l.SlaveLandIDs) > 0 {
			fullIDs = append(fullIDs, l.SlaveLandIDs...)
		}
	}
	fullIDs = dedupeInt64(fullIDs)
	// 枯死地块先铲除（对齐 Node autoPlantEmptyLands → removePlant）
	for _, id := range fullIDs {
		l := landByID[id]
		if l != nil && l.Plant != nil && len(l.Plant.Phases) > 0 {
			if ph := currentPhase(l.Plant.Phases, time.Now().Unix()); ph != nil && ph.Phase == proto.PhaseDead {
				_ = execFarmOp(c, "RemovePlant", proto.EncodeRemovePlantRequest([]int64{id}))
				time.Sleep(200 * time.Millisecond)
			}
		}
	}
	if err := ensureSeedOwned(c, seedID, 0, 0, len(fullIDs)); err != nil {
		return 0, err
	}
	if err := execFarmOp(c, "Plant", proto.EncodePlantRequest(seedID, fullIDs)); err != nil {
		return 0, err
	}
	recordOperation(accountID, "plant", int64(len(fullIDs)))
	appendOpLog(accountID, "plant", fmt.Sprintf("手动种植种子 %d → %d 块地", seedID, len(fullIDs)))
	return len(fullIDs), nil
}

// seedCand 商店候选种子（对齐 Node findBestSeed 的 candidate）
type seedCand struct {
	seedID  int64
	goodsID int64
	price   int64
	reqLvl  int
}

var errNoSeed = &seedErr{"no available seed"}

type seedErr struct{ msg string }

func (e *seedErr) Error() string { return e.msg }

// pickSeedForPlanting 按策略选种，返回 seedID / goodsID / price
func pickSeedForPlanting(accountID string, c *gw.Client, cfg config.AccountConfig, strategy string, needCount int) (int64, int64, int64, error) {
	// bag_priority：先消耗背包种子
	if strategy == "bag_priority" {
		if seedID, ok := pickBagSeed(accountID, c, cfg); ok {
			return seedID, 0, 0, nil
		}
		strategy = cfg.BagSeedFallbackStrategy
		if strategy == "" {
			strategy = "level"
		}
	}

	shop, err := c.Request(context.Background(), "gamepb.shoppb.ShopService", "ShopInfo",
		proto.EncodeShopInfoRequest(2), 15*time.Second)
	if err != nil {
		return 0, 0, 0, err
	}
	level := c.Level()
	var cands []seedCand
	candMap := map[int64]seedCand{}
	for _, g := range proto.DecodeShopInfoReply(shop.Body).GoodsList {
		if !g.Unlocked || g.ItemID <= 0 {
			continue
		}
		if g.LimitCount > 0 && g.BoughtNum >= g.LimitCount {
			continue
		}
		reqLvl := 0
		for _, cd := range g.Conds {
			if cd.Type == 1 { // MIN_LEVEL
				reqLvl = int(cd.Param)
			}
		}
		if reqLvl > 0 && int(level) < reqLvl {
			continue
		}
		if pe, ok := getPlantByID(g.ItemID); ok && pe.Size != 1 {
			continue // 仅 1x1 从商店买（2x2 走背包/优先）
		}
		cc := seedCand{seedID: g.ItemID, goodsID: g.ID, price: g.Price, reqLvl: reqLvl}
		cands = append(cands, cc)
		candMap[g.ItemID] = cc
	}
	if len(cands) == 0 {
		return 0, 0, 0, errNoSeed
	}

	switch strategy {
	case "preferred":
		if cfg.PreferredSeedID > 0 {
			if cc, ok := candMap[int64(cfg.PreferredSeedID)]; ok {
				return cc.seedID, cc.goodsID, cc.price, nil
			}
		}
		return bestByLevel(cands)
	case "level":
		return bestByLevel(cands)
	case "max_exp":
		return bestByRanking(cands, candMap, "exp", int64(level))
	case "max_fert_exp":
		return bestByRanking(cands, candMap, "fert", int64(level))
	case "max_profit":
		return bestByRanking(cands, candMap, "profit", int64(level))
	case "max_fert_profit":
		return bestByRanking(cands, candMap, "fert_profit", int64(level))
	default:
		return bestByLevel(cands)
	}
}

// bestByLevel 取等级门槛最高的候选
func bestByLevel(cands []seedCand) (int64, int64, int64, error) {
	if len(cands) == 0 {
		return 0, 0, 0, errNoSeed
	}
	best := cands[0]
	for _, cc := range cands[1:] {
		if cc.reqLvl > best.reqLvl {
			best = cc
		}
	}
	return best.seedID, best.goodsID, best.price, nil
}

// bestByRanking 按 getPlantRankings 的排序策略选第一个等级达标的候选
func bestByRanking(cands []seedCand, candMap map[int64]seedCand, sortBy string, userLevel int64) (int64, int64, int64, error) {
	rankings := getPlantRankings(sortBy)
	for _, r := range rankings {
		seedID, _ := r["seedId"].(float64)
		lvl, _ := r["level"].(float64)
		cc, ok := candMap[int64(seedID)]
		if !ok {
			continue
		}
		// 对齐 Node findBestSeed：等级门槛高于用户等级的候选跳过
		if int64(lvl) > userLevel {
			continue
		}
		return cc.seedID, cc.goodsID, cc.price, nil
	}
	return 0, 0, 0, errNoSeed
}

// pickBagSeed 背包优先：返回背包中可用的种子（count>0 且 1x1），按 BagSeedPriority 排序
func pickBagSeed(accountID string, c *gw.Client, cfg config.AccountConfig) (int64, bool) {
	rep, err := c.Request(context.Background(), "gamepb.itempb.ItemService", "Bag",
		proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		return 0, false
	}
	priority := map[int]int{}
	for i, s := range cfg.BagSeedPriority {
		priority[s] = i
	}
	type bagSeed struct {
		seedID int64
		count  int64
		reqLvl int
		prio   int
	}
	var seeds []bagSeed
	for _, it := range proto.DecodeBagReply(rep.Body).Items {
		if it.ID <= 0 || it.Count <= 0 || !isSeedItemID(it.ID) {
			continue
		}
		pe, ok := getPlantByID(it.ID)
		if !ok || pe.Size != 1 {
			continue
		}
		p, has := priority[int(it.ID)]
		if !has {
			p = 1 << 30
		}
		seeds = append(seeds, bagSeed{seedID: it.ID, count: it.Count, reqLvl: getSeedLevel(int(it.ID)), prio: p})
	}
	if len(seeds) == 0 {
		return 0, false
	}
	sort.SliceStable(seeds, func(i, j int) bool {
		if seeds[i].prio != seeds[j].prio {
			return seeds[i].prio < seeds[j].prio
		}
		return seeds[i].reqLvl > seeds[j].reqLvl
	})
	return seeds[0].seedID, true
}

// ensureSeedOwned 背包种子不足时从商店购买（对齐 Node buyGoods）
func ensureSeedOwned(c *gw.Client, seedID, goodsID, price int64, need int) error {
	rep, err := c.Request(context.Background(), "gamepb.itempb.ItemService", "Bag",
		proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		return err
	}
	have := int64(0)
	for _, it := range proto.DecodeBagReply(rep.Body).Items {
		if it.ID == seedID {
			have += it.Count
		}
	}
	if have >= int64(need) {
		return nil
	}
	buy := int64(need) - have
	if goodsID <= 0 || price <= 0 {
		shop, err := c.Request(context.Background(), "gamepb.shoppb.ShopService", "ShopInfo",
			proto.EncodeShopInfoRequest(2), 15*time.Second)
		if err != nil {
			return err
		}
		for _, g := range proto.DecodeShopInfoReply(shop.Body).GoodsList {
			if g.ItemID == seedID {
				goodsID, price = g.ID, g.Price
				break
			}
		}
	}
	if goodsID <= 0 {
		return errNoSeed
	}
	if _, err := c.Request(context.Background(), "gamepb.shoppb.ShopService", "BuyGoods",
		proto.EncodeBuyGoodsRequest(goodsID, buy, price), 12*time.Second); err != nil {
		return err
	}
	return nil
}

// ── 智能施肥（对齐 Node farm-fertilizer.js runFertilizerByConfig） ──

func normalizeFertilizerLandTypes(in []string) []string {
	valid := map[string]bool{"purple": true, "gold": true, "black": true, "red": true, "normal": true}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, t := range in {
		t = normalizeLandTypeName(t)
		if valid[t] && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func normalizeLandTypeName(t string) string {
	switch t {
	case "紫", "紫色", "紫土地":
		return "purple"
	case "金", "金色", "金土地":
		return "gold"
	case "黑", "黑土地":
		return "black"
	case "红", "红土地":
		return "red"
	case "普通", "普通地", "normal":
		return "normal"
	}
	return t
}

// landTypeByLevel 对齐 Node getLandTypeByLevel：5→purple,4→gold,3→black,2→red,else normal
func landTypeByLevel(level int64) string {
	switch level {
	case 5:
		return "purple"
	case 4:
		return "gold"
	case 3:
		return "black"
	case 2:
		return "red"
	default:
		return "normal"
	}
}

func runFertilizerByConfig(accountID string, c *gw.Client, cfg config.AccountConfig, explicitLandIDs []int64, reason string, skipNormal bool) {
	mode := cfg.Automation.Fertilizer
	if mode == "" || mode == "none" {
		return
	}
	landTypes := normalizeFertilizerLandTypes(cfg.Automation.FertilizerLandTypes)
	if len(landTypes) == 0 {
		return // 未选土地类型则不施肥
	}

	rep, err := c.Request(context.Background(), plantService, "AllLands",
		proto.EncodeAllLandsRequest(0), 15*time.Second)
	if err != nil {
		return
	}
	lands := proto.DecodeAllLandsReply(rep.Body).Lands
	now := time.Now().Unix()
	smartSeconds := cfg.Automation.FertilizerSmartSeconds
	if smartSeconds <= 0 {
		smartSeconds = 3600 // 对齐 Node runFertilizerByConfig 默认 3600
	}

	// 按土地类型过滤（从属地块随 master 处理：仅对非 slave 地块施肥）
	typeLandMap := map[int64]string{}
	var standalone []*proto.LandInfo
	for _, l := range lands {
		if l == nil || l.ID <= 0 || l.MasterLandID > 0 {
			continue
		}
		typeLandMap[l.ID] = landTypeByLevel(l.Level)
		standalone = append(standalone, l)
	}
	filterByType := func(ids []int64) []int64 {
		out := ids[:0:0]
		for _, id := range ids {
			if t, ok := typeLandMap[id]; ok {
				for _, want := range landTypes {
					if t == want {
						out = append(out, id)
						break
					}
				}
			}
		}
		return out
	}

	// 候选池：multi_season 用显式地块（对齐 Node explicitIds），其余用全部 standalone
	basePool := standaloneIDs(standalone)
	if reason == "multi_season" && len(explicitLandIDs) > 0 {
		basePool = explicitLandIDs
	}
	// 智能/最终阶段候选（对齐 Node getFastMatureLands / getFinalStageLands）
	fast := filterByType(getFastMatureLands(standalone, int64(smartSeconds), now))

	var normalTargets, organicTargets []int64
	switch mode {
	case "normal":
		if !skipNormal {
			normalTargets = filterByType(basePool)
		}
	case "organic":
		organicTargets = filterByType(basePool)
	case "both":
		if !skipNormal {
			normalTargets = filterByType(basePool)
		}
		organicTargets = filterByType(basePool)
	case "smart":
		// 普通(显式快成熟) + 有机(快成熟)，对齐 Node smart 分支
		if !skipNormal {
			normalTargets = fast
		}
		organicTargets = fast
	case "smart_only":
		organicTargets = fast
	case "smart_normal":
		if !skipNormal {
			normalTargets = fast
		}
	case "final_normal":
		if !skipNormal {
			normalTargets = filterByType(getFinalStageLands(standalone, false, now))
		}
	case "final_organic":
		organicTargets = filterByType(getFinalStageLands(standalone, true, now))
	}

	if len(normalTargets) > 0 {
		fertilizeLands(c, normalTargets, normalFertilizerID)
		recordOperation(accountID, "fertilize", int64(len(normalTargets)))
	}
	if len(organicTargets) > 0 {
		fertilizeOrganicLoop(c, organicTargets) // 有机肥：无次数即停止
		recordOperation(accountID, "fertilize", int64(len(organicTargets)))
	}
	if len(normalTargets) > 0 || len(organicTargets) > 0 {
		appendOpLog(accountID, "farm", fmt.Sprintf("施肥(%s) 普通%d/有机%d", mode, len(normalTargets), len(organicTargets)))
	}
}

func standaloneIDs(lands []*proto.LandInfo) []int64 {
	out := make([]int64, 0, len(lands))
	for _, l := range lands {
		out = append(out, l.ID)
	}
	return out
}

// getFastMatureLands 对齐 Node getFastMatureLands：MATURE begin_time 在 [0, threshold] 内且未枯死
func getFastMatureLands(lands []*proto.LandInfo, thresholdSec, now int64) []int64 {
	out := make([]int64, 0, len(lands))
	for _, l := range lands {
		if l == nil || l.Plant == nil || len(l.Plant.Phases) == 0 {
			continue
		}
		ph := currentPhase(l.Plant.Phases, now)
		if ph == nil || ph.Phase == proto.PhaseDead || ph.Phase == proto.PhaseMature {
			continue
		}
		for _, p := range l.Plant.Phases {
			if p.Phase == proto.PhaseMature && p.BeginTime > 0 {
				if p.BeginTime-now >= 0 && p.BeginTime-now <= thresholdSec {
					out = append(out, l.ID)
				}
				break
			}
		}
	}
	return out
}

// getFinalStageLands 对齐 Node getFinalStageLands：当前阶段恰为 MATURE 前一阶段
func getFinalStageLands(lands []*proto.LandInfo, organicOnly bool, now int64) []int64 {
	_ = organicOnly // Go 侧未下发 left_inorc_fert_times，有机/普通统一按阶段筛选
	out := make([]int64, 0, len(lands))
	for _, l := range lands {
		if l == nil || l.Plant == nil || len(l.Plant.Phases) == 0 {
			continue
		}
		phases := append([]*proto.PlantPhaseInfo{}, l.Plant.Phases...)
		sort.SliceStable(phases, func(i, j int) bool { return phases[i].BeginTime < phases[j].BeginTime })
		matureIdx := -1
		for i, p := range phases {
			if p.Phase == proto.PhaseMature {
				matureIdx = i
				break
			}
		}
		if matureIdx <= 0 {
			continue
		}
		cur := currentPhase(l.Plant.Phases, now)
		if cur == nil {
			continue
		}
		curIdx := -1
		for i, p := range phases {
			if p == cur {
				curIdx = i
				break
			}
		}
		if curIdx == matureIdx-1 {
			out = append(out, l.ID)
		}
	}
	return out
}

func fertilizeLands(c *gw.Client, ids []int64, fertID int64) {
	for _, id := range ids {
		_ = execFarmOp(c, "Fertilize", proto.EncodeFertilizeRequest([]int64{id}, fertID))
		time.Sleep(200 * time.Millisecond)
	}
}

// fertilizeOrganicLoop 对齐 Node fertilizeOrganicLoop：逐块施有机肥，无次数即停止
func fertilizeOrganicLoop(c *gw.Client, ids []int64) {
	for _, id := range ids {
		if err := execFarmOp(c, "Fertilize", proto.EncodeFertilizeRequest([]int64{id}, organicFertilizerID)); err != nil {
			return // 首次失败（无有机化肥次数）即停止
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// ── 自动卖果实（对齐 Node worker.js farmHarvested → sellAllFruits） ──

func autoSellAfterHarvest(accountID string, c *gw.Client) {
	rep, err := c.Request(context.Background(), "gamepb.itempb.ItemService", "Bag",
		proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		return
	}
	items := proto.DecodeBagReply(rep.Body).Items
	sell := make([]proto.SellItem, 0, len(items))
	for _, it := range items {
		if it.ID > 0 && it.Count > 0 && isFruitItemID(it.ID) {
			sell = append(sell, proto.SellItem{ID: it.ID, Count: it.Count, UID: it.UID})
		}
	}
	if len(sell) == 0 {
		return
	}
	if _, err := c.Request(context.Background(), "gamepb.itempb.ItemService", "Sell",
		proto.EncodeSellRequest(sell), 12*time.Second); err != nil {
		appendOpLog(accountID, "farm", "自动卖果实失败: "+err.Error())
		return
	}
	recordOperation(accountID, "sell", int64(len(sell)))
	appendOpLog(accountID, "farm", fmt.Sprintf("自动卖果实 %d 种", len(sell)))
}

// ── 自动买化肥（对齐 Node farm-scheduler.js + mall.js checkAndBuyFertilizerBoth） ──
// 化肥容器：普通 1011 / 有机 1012（count 为秒，/3600 为小时）；商品：有机 1002 / 普通 1003（slot_type=1）

func fertilizeBuyLoop(accountID string, stop chan struct{}) {
	cfg := models.GetAccountConfig(accountID)
	ivMin := cfg.FertilizerBuyCheckIntervalMinutes
	if ivMin <= 0 {
		ivMin = 60
	}
	ticker := time.NewTicker(time.Duration(ivMin) * time.Minute)
	defer ticker.Stop()
	select {
	case <-stop:
		return
	case <-time.After(30 * time.Second):
	}
	for {
		cfg = models.GetAccountConfig(accountID)
		if cfg.Automation.FertilizerBuyOrganic || cfg.Automation.FertilizerBuyNormal {
			if c, err := clientPool.Get(accountID); err == nil && c != nil {
				doCheckAndBuyFertilizer(accountID, c, cfg)
			}
		}
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

func doCheckAndBuyFertilizer(accountID string, c *gw.Client, cfg config.AccountConfig) {
	rep, err := c.Request(context.Background(), "gamepb.itempb.ItemService", "Bag",
		proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		return
	}
	hoursNormal, hoursOrganic := 0.0, 0.0
	for _, it := range proto.DecodeBagReply(rep.Body).Items {
		switch it.ID {
		case normalFertilizerID:
			hoursNormal = float64(it.Count) / 3600
		case organicFertilizerID:
			hoursOrganic = float64(it.Count) / 3600
		}
	}

	bought := false
	if cfg.Automation.FertilizerBuyOrganic && cfg.FertilizerBuyOrganicCount > 0 &&
		cfg.FertilizerBuyOrganicThresholdHours > 0 && hoursOrganic < float64(cfg.FertilizerBuyOrganicThresholdHours) {
		if buyMallFertilizer(c, 1002, int64(cfg.FertilizerBuyOrganicCount)) {
			bought = true
		}
	}
	if cfg.Automation.FertilizerBuyNormal && cfg.FertilizerBuyNormalCount > 0 &&
		cfg.FertilizerBuyNormalThresholdHours > 0 && hoursNormal < float64(cfg.FertilizerBuyNormalThresholdHours) {
		if bought {
			time.Sleep(time.Duration(1000+rand.Intn(2000)) * time.Millisecond) // 有机/普通间 1–3s
		}
		buyMallFertilizer(c, 1003, int64(cfg.FertilizerBuyNormalCount))
	}
	if bought {
		appendOpLog(accountID, "farm", "自动购买化肥")
	}
}

func buyMallFertilizer(c *gw.Client, goodsID, count int64) bool {
	rep, err := c.Request(context.Background(), "gamepb.mallpb.MallService", "GetMallListBySlotType",
		proto.EncodeGetMallListBySlotTypeRequest(1), 12*time.Second)
	if err != nil {
		return false
	}
	found := false
	for _, g := range proto.DecodeMallListBySlotTypeReply(rep.Body).GoodsList {
		if g.GoodsID == goodsID {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if _, err := c.Request(context.Background(), "gamepb.mallpb.MallService", "Purchase",
		proto.EncodePurchaseRequest(goodsID, count), 12*time.Second); err != nil {
		return false
	}
	return true
}
