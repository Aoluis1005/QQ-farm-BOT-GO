package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/models"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

func registerProfileAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/farm/lands", handleFarmLands)
	mux.HandleFunc("/api/farm/harvest", handleFarmHarvest)
	mux.HandleFunc("/api/farm/action", handleFarmAction)
	mux.HandleFunc("/api/farm/plant", handleFarmPlant)
	mux.HandleFunc("/api/bag", handleBagItems)       // 
	mux.HandleFunc("/api/bag/items", handleBagItems) // 兼容旧路径
	mux.HandleFunc("/api/bag/seeds", handleBagSeeds)
	mux.HandleFunc("/api/bag/use", handleBagUse)
	mux.HandleFunc("/api/bag/sell", handleBagSell)
	mux.HandleFunc("/api/friends/list", handleFriendList)
	mux.HandleFunc("/api/friends/clear-cache", handleFriendListCacheClear)
	mux.HandleFunc("/api/friends/lands", handleFriendLandsRoute)
	mux.HandleFunc("/api/friends/blacklist", handleFriendBlacklist)
	mux.HandleFunc("/api/friends/requests", handleFriendRequests)
	mux.HandleFunc("/api/friends/visitors", handleFriendVisitors)
}

func handleFarmLands(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	if accountID == "" {
		accountID = "default"
	}

	// 连接网关拉真实农场数据
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	force := r.URL.Query().Get("refresh") == "1" // 操作后强制刷新
	body, ok := c.LandsCached(30 * time.Second)  // 优先读预拉缓存，加速首页
	if force || !ok {
		rep, err := c.Request(r.Context(), "gamepb.plantpb.PlantService", "AllLands",
			proto.EncodeAllLandsRequest(0), 15*time.Second)
		if err != nil {
			writeError(w, 500, "拉取农场失败: "+err.Error())
			return
		}
		body = rep.Body
		c.StoreLands(body) // 更新缓存，后续读缓存即最新
	}
	all := proto.DecodeAllLandsReply(body)

	// ── ，逐字段构造 ──
	serverTime := time.Now().Unix()
	landMap := buildFarmLandMap(all.Lands)

	details := make([]map[string]interface{}, 0, len(all.Lands))
	for _, l := range all.Lands {
		landID := l.ID
		level := l.Level
		maxLevel := l.MaxLevel
		landsLevel := l.LandsLevel
		landSize := l.LandSize
		landTypeName := farmLandTypeNameByLevel(level)
		couldUnlock := l.CouldUnlock
		couldUpgrade := l.CouldUpgrade

		ctx := getFarmDisplayLandContext(l, landMap)

		// 未解锁
		if !l.Unlocked {
			details = append(details, map[string]interface{}{
				"id": landID, "unlocked": false, "status": "locked",
				"plantName": "", "phaseName": "",
				"level": level, "maxLevel": maxLevel, "landsLevel": landsLevel,
				"landSize": landSize, "landTypeName": landTypeName,
				"couldUnlock": couldUnlock, "couldUpgrade": couldUpgrade,
				"currentSeason": 0, "totalSeason": 0,
				"occupiedByMaster": false, "masterLandId": int64(0),
				"occupiedLandIds": []int64{}, "plantSize": int64(1),
			})
			continue
		}

		plant := ctx.SourceLand.Plant

		// 空地
		if plant == nil || len(plant.Phases) == 0 {
			details = append(details, map[string]interface{}{
				"id": landID, "unlocked": true, "status": "empty",
				"plantName": "", "phaseName": "空地",
				"level": level, "maxLevel": maxLevel, "landsLevel": landsLevel,
				"landSize": landSize, "landTypeName": landTypeName,
				"couldUnlock": couldUnlock, "couldUpgrade": couldUpgrade,
				"currentSeason": 0, "totalSeason": 0,
				"occupiedByMaster": ctx.OccupiedByMaster, "masterLandId": ctx.MasterLandID,
				"occupiedLandIds": ctx.OccupiedLandIDs, "plantSize": int64(1),
			})
			continue
		}

		currentPhase := farmCurrentPhase(plant.Phases, serverTime)
		if currentPhase == nil {
			details = append(details, map[string]interface{}{
				"id": landID, "unlocked": true, "status": "empty",
				"plantName": "", "phaseName": "",
				"level": level, "maxLevel": maxLevel, "landsLevel": landsLevel,
				"landSize": landSize, "landTypeName": landTypeName,
				"couldUnlock": couldUnlock, "couldUpgrade": couldUpgrade,
				"currentSeason": 0, "totalSeason": 0,
				"occupiedByMaster": ctx.OccupiedByMaster, "masterLandId": ctx.MasterLandID,
				"occupiedLandIds": ctx.OccupiedLandIDs, "plantSize": int64(1),
			})
			continue
		}

		phase := currentPhase.Phase
		plantID := plant.ID
		displayName := getPlantNameOrNull(plantID)
		if displayName == "" {
			displayName = plant.Name
		}
		if displayName == "" {
			displayName = "未知"
		}
		plantInfo, _ := getPlantByID(plantID)
		seedID := int64(0)
		if plantInfo.SeedID > 0 {
			seedID = int64(plantInfo.SeedID)
		}
		seedImage := ""
		if seedID > 0 {
			seedImage = GetItemImageURL(int(seedID))
		}
		plantSize := int64(1)
		if plantInfo.Size > 1 {
			plantSize = int64(plantInfo.Size)
		}
		totalSeason := int64(1)
		if plantInfo.Seasons > 1 {
			totalSeason = int64(plantInfo.Seasons)
		}
		rawSeason := plant.Season
		currentSeason := int64(1)
		if rawSeason > 0 {
			currentSeason = rawSeason
			if currentSeason > totalSeason {
				currentSeason = totalSeason
			}
		}
		phaseName := farmPhaseName(phase)

		// 剩余成熟时间：，减去服务器时间
		matureInSec := int64(0)
		for _, ph := range plant.Phases {
			if ph.Phase == proto.PhaseMature && ph.BeginTime > serverTime {
				matureInSec = ph.BeginTime - serverTime
				break
			}
		}
		totalGrowTime := getPlantGrowTime(plantID)

		// 状态
		status := "growing"
		if phase == proto.PhaseMature {
			status = "harvestable"
		} else if phase == proto.PhaseDead {
			status = "dead"
		}

		// 需要浇水/除草/除虫
		needWater := plant.DryNum > 0 || (currentPhase.DryTime > 0 && currentPhase.DryTime <= serverTime)
		needWeed := len(plant.WeedOwners) > 0 || (currentPhase.WeedsTime > 0 && currentPhase.WeedsTime <= serverTime)
		needBug := len(plant.InsectOwners) > 0 || (currentPhase.InsectTime > 0 && currentPhase.InsectTime <= serverTime)

		// 变异效果
		mutantEffects := getMutantEffectsByIDs(plant.MutantConfigIDs)
		me := make([]map[string]interface{}, 0, len(mutantEffects))
		for _, e := range mutantEffects {
			item := map[string]interface{}{"id": e.ID, "name": e.Name, "effect_name": e.EffectName, "icon": e.Icon, "tag": e.Tag}
			nm := e.Name
			if nm == "" && e.EffectName != "" {
				nm = e.EffectName
			}
			if nm == "" {
				nm = "变异"
			}
			item["name"] = nm
			me = append(me, item)
		}

		details = append(details, map[string]interface{}{
			"id": landID, "unlocked": true, "status": status,
			"plantName": displayName, "plantId": plantID, "seedId": seedID, "seedImage": seedImage,
			"phaseName": phaseName, "currentSeason": currentSeason, "totalSeason": totalSeason,
			"matureInSec": matureInSec, "totalGrowTime": totalGrowTime,
			"needWater": needWater, "needWeed": needWeed, "needBug": needBug,
			"stealable": plant.Stealable,
			"level":     level, "maxLevel": maxLevel, "landsLevel": landsLevel,
			"landSize": landSize, "landTypeName": landTypeName,
			"couldUnlock": couldUnlock, "couldUpgrade": couldUpgrade,
			"occupiedByMaster": ctx.OccupiedByMaster, "masterLandId": ctx.MasterLandID,
			"occupiedLandIds": ctx.OccupiedLandIDs,
			"plantSize":       plantSize, "mutantEffects": me,
		})
	}

	writeJSON(w, map[string]interface{}{"ok": true, "data": map[string]interface{}{
		"lands":   details,
		"summary": summarizeFarmLands(details),
	}})
}

// ── 以下辅助函数

// farmLandTypeNameByLevel 土地类型名
func farmLandTypeNameByLevel(level int64) string {
	switch level {
	case 5:
		return "紫土地"
	case 4:
		return "金土地"
	case 3:
		return "黑土地"
	case 2:
		return "红土地"
	default:
		return "普通地"
	}
}

// farmPhaseName 阶段名
func farmPhaseName(phase int32) string {
	names := [...]string{"未知", "种子", "发芽", "小叶", "大叶", "开花", "成熟", "枯死"}
	if phase >= 0 && int(phase) < len(names) {
		return names[phase]
	}
	return ""
}

// farmCurrentPhase 当前所处阶段
func farmCurrentPhase(phases []*proto.PlantPhaseInfo, serverTime int64) *proto.PlantPhaseInfo {
	if len(phases) == 0 {
		return nil
	}
	for i := len(phases) - 1; i >= 0; i-- {
		if phases[i].BeginTime > 0 && phases[i].BeginTime <= serverTime {
			return phases[i]
		}
	}
	// 所有阶段都在未来，使用第一个
	return phases[0]
}

// farmLandMap id → land
func buildFarmLandMap(lands []*proto.LandInfo) map[int64]*proto.LandInfo {
	m := make(map[int64]*proto.LandInfo, len(lands))
	for _, l := range lands {
		if l != nil && l.ID > 0 {
			m[l.ID] = l
		}
	}
	return m
}

// farmSlaveLandIDs 从属土地 ID 列表
func farmSlaveLandIDs(land *proto.LandInfo) []int64 {
	seen := map[int64]bool{}
	out := []int64{}
	for _, id := range land.SlaveLandIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// farmHasPlantData 是否有植物数据
func farmHasPlantData(land *proto.LandInfo) bool {
	return land != nil && land.Plant != nil && len(land.Plant.Phases) > 0
}

// farmLinkedMasterLand 关联的主土地
func farmLinkedMasterLand(land *proto.LandInfo, landMap map[int64]*proto.LandInfo) *proto.LandInfo {
	landID := land.ID
	masterID := land.MasterLandID
	if masterID <= 0 || masterID == landID {
		return nil
	}
	master := landMap[masterID]
	if master == nil {
		return nil
	}
	slaveIDs := farmSlaveLandIDs(master)
	if len(slaveIDs) > 0 && !containsInt64(slaveIDs, landID) {
		return nil
	}
	return master
}

// farmDisplayLandContext 显示上下文
type farmLandContext struct {
	SourceLand       *proto.LandInfo
	OccupiedByMaster bool
	MasterLandID     int64
	OccupiedLandIDs  []int64
}

func getFarmDisplayLandContext(land *proto.LandInfo, landMap map[int64]*proto.LandInfo) farmLandContext {
	if master := farmLinkedMasterLand(land, landMap); master != nil && farmHasPlantData(master) {
		allIDs := []int64{master.ID}
		allIDs = append(allIDs, farmSlaveLandIDs(master)...)
		filtered := []int64{}
		for _, id := range allIDs {
			if id > 0 {
				filtered = append(filtered, id)
			}
		}
		occ := filtered
		if len(filtered) <= 1 {
			occ = filtered
		}
		return farmLandContext{SourceLand: master, OccupiedByMaster: true, MasterLandID: master.ID, OccupiedLandIDs: occ}
	}
	landID := land.ID
	return farmLandContext{SourceLand: land, OccupiedByMaster: false, MasterLandID: landID, OccupiedLandIDs: []int64{landID}}
}

// summarizeFarmLands 汇总
func summarizeFarmLands(lands []map[string]interface{}) map[string]int {
	s := map[string]int{"harvestable": 0, "growing": 0, "empty": 0, "dead": 0, "needWater": 0, "needWeed": 0, "needBug": 0}
	for _, land := range lands {
		unlocked, _ := land["unlocked"].(bool)
		if !unlocked {
			continue
		}
		status, _ := land["status"].(string)
		switch status {
		case "harvestable":
			s["harvestable"]++
		case "dead":
			s["dead"]++
		case "empty":
			s["empty"]++
		case "growing", "stealable", "harvested":
			s["growing"]++
		}
		if needWater, _ := land["needWater"].(bool); needWater {
			s["needWater"]++
		}
		if needWeed, _ := land["needWeed"].(bool); needWeed {
			s["needWeed"]++
		}
		if needBug, _ := land["needBug"].(bool); needBug {
			s["needBug"]++
		}
	}
	return s
}

// analyzeLand 分析地块状态：ready/growing/dry/dead/idle + 作物名 + 进度 + 剩余时间
func analyzeLand(l *proto.LandInfo, now int64) (status, name string, progress int, timeLeft string) {
	if l.Plant == nil {
		return "idle", "", 0, ""
	}
	p := l.Plant
	name = p.Name
	status = "growing"

	// 缺水/虫/草覆盖
	if p.DryNum > 0 {
		status = "dry"
	}

	// 找当前阶段（begin_time<=now 的最大）
	var current *proto.PlantPhaseInfo
	for _, ph := range p.Phases {
		if ph.BeginTime > 0 && ph.BeginTime <= now {
			current = ph
		}
	}
	if current == nil && len(p.Phases) > 0 {
		current = p.Phases[0]
	}

	if current != nil {
		switch current.Phase {
		case proto.PhaseMature:
			status = "ready"
			progress = 100
		case proto.PhaseDead:
			status = "dead"
			progress = 0
		default:
			// 生长中：进度按阶段位置估算，timeLeft=到下一阶段
			idx := 0
			for i, ph := range p.Phases {
				if ph == current {
					idx = i
				}
			}
			if len(p.Phases) > 1 {
				progress = (idx * 100) / (len(p.Phases) - 1)
			} else {
				progress = 10
			}
			if progress < 5 {
				progress = 5
			}
			// 下一个阶段起止时间 → 剩余
			for _, ph := range p.Phases {
				if ph.BeginTime > now {
					timeLeft = fmtDur(ph.BeginTime - now)
					break
				}
			}
		}
	}

	// 若缺水，进度保持展示但状态为 dry
	return status, name, progress, timeLeft
}

func fruitCount(p *proto.PlantInfo) int64 {
	if p == nil {
		return 0
	}
	return p.FruitNum
}

// iconFor 作物名 → emoji（未知用默认 🌱）
func iconFor(name string) string {

	switch name {
	case "草莓":
		return "🍓"
	case "番茄", "西红柿":
		return "🍅"
	case "葡萄":
		return "🍇"
	case "玉米":
		return "🌽"
	case "胡萝卜":
		return "🥕"
	case "向日葵", "玫瑰花", "红玫瑰", "白玫瑰", "紫玫瑰", "郁金香", "荷花":
		return "🌻"
	case "茄子":
		return "🍆"
	case "白菜", "卷心菜", "大白菜":
		return "🥬"
	case "西瓜":
		return "🍉"
	case "苹果":
		return "🍎"
	case "梨", "桃子":
		return "🍑"
	case "橙子", "柑橘", "柚子":
		return "🍊"
	case "香蕉":
		return "🍌"
	case "菠萝":
		return "🍍"
	case "椰子":
		return "🥥"
	case "樱桃":
		return "🍒"
	case "蓝莓":
		return "🫐"
	case "柠檬":
		return "🍋"
	case "芒果":
		return "🥭"
	case "南瓜":
		return "🎃"
	case "土豆":
		return "🥔"
	case "红薯", "白萝卜", "萝卜":
		return "🥔"
	case "辣椒":
		return "🌶️"
	case "豌豆", "大豆", "绿豆", "黄豆":
		return "🫛"
	case "蘑菇":
		return "🍄"
	case "小麦", "水稻", "稻谷":
		return "🌾"
	case "甘蔗":
		return "🎋"
	default:
		return "🌱"
	}
}

// fmtDur 秒 → 人类可读剩余时间
func fmtDur(sec int64) string {
	if sec <= 0 {
		return ""
	}
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	if d > 0 {
		return fmt.Sprintf("%dd%dh", d, h)
	}
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%ds", sec)
}

func handleFarmHarvest(w http.ResponseWriter, r *http.Request) {
	landID := r.FormValue("landId")
	if landID == "" {
		writeError(w, 400, "missing landId")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	ids := parseIDs(landID)
	if len(ids) == 0 {
		writeError(w, 400, "bad landId")
		return
	}
	if err := execFarmOp(c, "Harvest", proto.EncodeHarvestRequest(ids, c.GID, true)); err != nil {
		writeError(w, 500, "收获失败: "+err.Error())
		return
	}
	recordOperation(accountID, "harvest", int64(len(ids)))
	appendOpLog(accountID, "harvest", fmt.Sprintf("手动收获 %d 块地", len(ids)))
	// 收获后可选自动卖
	if models.GetAccountConfig(accountID).Automation.Sell {
		autoSellAfterHarvest(accountID, c)
	}
	writeJSON(w, map[string]interface{}{"ok": true, "landIds": ids, "message": fmt.Sprintf("收获 %d 块地", len(ids))})
}

func handleFarmPlant(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	landID := r.FormValue("landId")
	seedIDStr := r.FormValue("seedId")
	if landID == "" || seedIDStr == "" {
		writeError(w, 400, "missing landId or seedId")
		return
	}
	seedID, err := strconv.ParseInt(seedIDStr, 10, 64)
	if err != nil {
		writeError(w, 400, "bad seedId")
		return
	}
	landIDs := parseIDs(landID)
	if len(landIDs) == 0 {
		writeError(w, 400, "bad landId")
		return
	}
	n, err := plantOnLands(accountID, c, seedID, landIDs)
	if err != nil {
		writeError(w, 500, "种植失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "seedId": seedID, "landIds": landIDs, "message": fmt.Sprintf("种植 %d 块地", n)})
}

func handleBagItems(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	// 真实背包数据：
	rep, err := c.Request(r.Context(), "gamepb.itempb.ItemService", "Bag",
		proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		writeError(w, 500, "拉取背包失败: "+err.Error())
		return
	}
	br := proto.DecodeBagReply(rep.Body)
	c.ApplyBagAssets(br)

	type bagOut struct {
		ID              int64  `json:"id"`
		Name            string `json:"name"`
		Count           int64  `json:"count"`
		Category        string `json:"category"`
		Img             string `json:"img,omitempty"`
		Icon            string `json:"icon,omitempty"`
		ItemType        int64  `json:"itemType"`        // 6/17=果实可售, 11=道具可用
		UID             int64  `json:"uid"`             // 物品实例 uid，出售时回传
		Price           int64  `json:"price"`           // 
		PriceID         int64  `json:"priceId"`         // 
		PriceUnit       string `json:"priceUnit"`       // 1005=金豆豆/200=点券/else金
		Level           int64  `json:"level"`           // 
		InteractionType string `json:"interactionType"` // 
		HoursText       string `json:"hoursText"`       // （默认空）
	}
	items := make([]bagOut, 0, len(br.Items))
	for _, it := range br.Items {
		if it.ID <= 0 || it.Count <= 0 {
			continue
		}
		cat, name := classifyBagCategory(it.ID)
		entry := itemInfoMap[int(it.ID)]
		priceUnit := "金"
		if entry.PriceID == 1005 {
			priceUnit = "金豆豆"
		} else if entry.PriceID == 200 {
			priceUnit = "点券"
		}
		outItem := bagOut{ID: it.ID, Name: name, Count: it.Count, Category: cat,
			ItemType: int64(entry.Type), UID: it.UID,
			Price: int64(entry.Price), PriceID: int64(entry.PriceID), PriceUnit: priceUnit,
			Level: int64(entry.Level), InteractionType: entry.InteractionType, HoursText: ""}
		if img := GetItemImageURL(int(it.ID)); img != "" {
			outItem.Img = img
		} else {
			outItem.Icon = iconFor(name)
		}
		items = append(items, outItem)
	}

	// 排序：果实→种子→化肥→道具→其他，同类数量降序
	catOrder := map[string]int{"fruit": 0, "seed": 1, "fertilizer": 2, "props": 3, "other": 4}
	sort.SliceStable(items, func(i, j int) bool {
		oi, oj := catOrder[items[i].Category], catOrder[items[j].Category]
		if oi != oj {
			return oi < oj
		}
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].ID < items[j].ID
	})

	writeJSON(w, map[string]interface{}{"ok": true, "data": items})
}

// handleBagSeeds GET /api/bag/seeds
// 仅返回背包中实际拥有的种子（id>0 且 count>0 且 Plant.json 收录），
// 含数量/尺寸(2x2)/所需等级，供"背包种子优先顺序"面板使用（而非全种子库）。
func handleBagSeeds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	rep, err := c.Request(r.Context(), "gamepb.itempb.ItemService", "Bag",
		proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		writeError(w, 500, "拉取背包失败: "+err.Error())
		return
	}
	br := proto.DecodeBagReply(rep.Body)
	c.ApplyBagAssets(br)

	type bagSeedOut struct {
		SeedID        int    `json:"seedId"`
		Name          string `json:"name"`
		Count         int64  `json:"count"`
		RequiredLevel int    `json:"requiredLevel"`
		PlantSize     int    `json:"plantSize"`
		Image         string `json:"image,omitempty"`
	}
	seedMap := map[int]*bagSeedOut{}
	for _, it := range br.Items {
		id := int(it.ID)
		count := it.Count
		if id <= 0 || count <= 0 {
			continue
		}
		// 返回背包实际拥有的全部种子（含活动种子）供"背包种子优先顺序"面板，不再排除活动种子/黑名单
		// 仅纳入 Plant.json 收录的种子物品（无 plant 条目则跳过；交互类型为 plant 但缺配置时记日志）
		plant, ok := seedToPlantMap[id]
		if !ok {
			if isSeedItemID(int64(id)) {
				log.Printf("[bag] 背包种子 %d 未收录进 Plant.json，已忽略", id)
			}
			continue
		}
		name := plant.Name
		// 去 "??" 后缀（('??') 处理）
		if strings.HasSuffix(name, "??") {
			name = name[:len(name)-2]
		}
		if name == "" {
			name = seedPlantName(int64(id))
		}
		if name == "" {
			name = "种子" + strconv.Itoa(id)
		}
		// requiredLevel：(0, plant.land_level_need || info.level || getSeedLevel(id))
		// Go 无 land_level_need 字段，用 getSeedLevel(itemInfo.level) 作为权威值
		reqLvl := getSeedLevel(id)
		if reqLvl < 0 {
			reqLvl = 0
		}
		plantSize := plant.Size
		if plantSize <= 0 {
			plantSize = 1
		}
		img := getSeedImageBySeedID(id)
		if img == "" {
			img = GetItemImageURL(id)
		}
		if ex, ok := seedMap[id]; ok {
			ex.Count += count
		} else {
			seedMap[id] = &bagSeedOut{
				SeedID:        id,
				Name:          name,
				Count:         count,
				RequiredLevel: reqLvl,
				PlantSize:     plantSize,
				Image:         img,
			}
		}
	}
	out := make([]bagSeedOut, 0, len(seedMap))
	for _, s := range seedMap {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SeedID < out[j].SeedID })
	writeJSON(w, map[string]interface{}{"ok": true, "data": out})
}

// handleBagUse POST /api/bag/use 
func handleBagUse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		ItemID int64 `json:"itemId"`
		Count  int64 `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	if req.ItemID <= 0 {
		writeError(w, 400, "缺少 itemId")
		return
	}
	if req.Count <= 0 {
		req.Count = 1
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	// 先发标准 UseRequest；若报 code=1000020 /
	// 请求参数错误，则改用 raw protobuf 嵌套形态重发一次（服务端实际期望的结构）。
	_, err = c.Request(r.Context(), "gamepb.itempb.ItemService", "Use",
		proto.EncodeUseRequest(req.ItemID, req.Count), 12*time.Second)
	if err != nil && proto.IsBadParamError(err.Error()) {
		_, err = c.Request(r.Context(), "gamepb.itempb.ItemService", "Use",
			proto.EncodeUseRequestFallback(req.ItemID, req.Count), 12*time.Second)
	}
	if err != nil {
		writeError(w, 500, "使用失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "message": "use ok"})
}

// handleBagSell POST /api/bag/sell 
func handleBagSell(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		Items []struct {
			ID    int64 `json:"id"`
			Count int64 `json:"count"`
			UID   int64 `json:"uid"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	items := make([]proto.SellItem, 0, len(req.Items))
	for _, it := range req.Items {
		if it.ID <= 0 || it.Count <= 0 {
			continue
		}
		items = append(items, proto.SellItem{ID: it.ID, Count: it.Count, UID: it.UID})
	}
	if len(items) == 0 {
		writeError(w, 400, "items 无效")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	rep, err := c.Request(r.Context(), "gamepb.itempb.ItemService", "Sell",
		proto.EncodeSellRequest(items), 12*time.Second)
	if err != nil {
		writeError(w, 500, "出售失败: "+err.Error())
		return
	}
	// 解析卖果实金币收益（('sell')）
	soldCount, gold := proto.DecodeSellReply(rep.Body)
	// 手动出售也按金币记录 sell 操作计数
	if gold > 0 {
		recordOperation(accountID, "sell", gold)
	}
	writeJSON(w, map[string]interface{}{"ok": true, "message": "sell ok", "gold": gold, "count": soldCount})
}

// classifyBagCategory 。
// 前端分类 tab：fruit/seed/props(=props+fertilizer)/other。分类判定走 game_config.go（源于 Plant.json+ItemInfo.json）。
func classifyBagCategory(id int64) (category, name string) {
	switch id {
	case 1001:
		return "other", "金币"
	case 1002:
		return "other", "经验"
	}
	if isFruitItemID(id) {
		n := fruitPlantName(id)
		if n == "" {
			n = "果实" + itoa(id)
		}
		return "fruit", n
	}
	if isSeedItemID(id) {
		n := seedPlantName(id)
		if n == "" {
			n = "种子" + itoa(id)
		}
		return "seed", n
	}
	if isFertilizerItemID(id) {
		n := itemDisplayName(id)
		if n == "" {
			n = itoa(id)
		}
		return "fertilizer", n
	}
	n := itemDisplayName(id)
	if n == "" {
		n = "物品" + itoa(id)
	}
	return "props", n
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// handleFriendList 真实好友列表：
// 仅调用 FriendService/GetAll（或 QQ 的 GetGameFriends），【不进入任何好友农场】。
// 护主犬(dogId)来自本地狗信息缓存（由 fetch-dog-info / 巡查时 Enter 收集），
// 可偷/可帮忙摘要直接取自 GetAll 响应的 friend.plant 字段。
func handleFriendList(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	// 展示用好友列表走 TTL 缓存；?forceSync=true 强制刷新绕过缓存
	forceSync := r.URL.Query().Get("forceSync") == "true"
	platform := ""
	if acc := models.GetAccountByID(accountID); acc != nil {
		platform = acc.Platform
	}
	knownGids := models.GetAccountConfig(accountID).KnownFriendGIDs
	allFriends, err := getAllFriendsCached(c, accountID, platform, knownGids, forceSync)
	if err != nil {
		writeError(w, 500, "拉取好友失败: "+err.Error())
		return
	}

	myGID := c.GID
	// 黑名单取自本地文件（与 toggle/blacklist tab 同源），
	blackMap := readBlacklist(accountID)
	// 护主犬缓存
	dogMap, _ := readDogCache(accountID)

	friends := make([]map[string]interface{}, 0, len(allFriends))
	for _, f := range allFriends {
		if f.GID <= 0 || f.GID == myGID {
			continue
		}
		// 排除假 NPC
		if (f.Name == "小小农夫" || f.Remark == "小小农夫") && f.Level == 1 {
			continue
		}

		name := firstNonEmpty(f.Remark, f.Name, fmt.Sprintf("GID:%d", f.GID))
		item := map[string]interface{}{
			"uid":        f.GID,
			"gid":        f.GID,
			"name":       name,
			"avatar":     f.AvatarURL,
			"level":      f.Level,
			"coins":      f.Gold,
			"hasDog":     false,
			"dogId":      int64(0),
			"dogName":    "",
			"canSteal":   false,
			"canHelp":    false,
			"canBad":     true,
			"ripeLands":  0,
			"totalLands": 0,
			"tip":        "",
		}

		// 护主犬：本地缓存优先
		if d, ok := dogMap[f.GID]; ok {
			item["hasDog"] = d.DogID > 0
			item["dogId"] = d.DogID
			item["dogName"] = d.DogName
		}

		// 地块摘要：直接取自 GetAll 响应的 plant 字段（不进农场）
		if f.Plant != nil {
			steal := f.Plant.StealPlantNum
			dry := f.Plant.DryNum
			weed := f.Plant.WeedNum
			insect := f.Plant.InsectNum
			item["plant"] = map[string]interface{}{
				"stealNum":  steal,
				"dryNum":    dry,
				"weedNum":   weed,
				"insectNum": insect,
			}
			item["ripeLands"] = steal
			item["canSteal"] = steal > 0
			item["canHelp"] = (dry + weed + insect) > 0
			if steal > 0 {
				item["tip"] = fmt.Sprintf("可偷 %d 块", steal)
			} else if (dry + weed + insect) > 0 {
				item["tip"] = "可帮忙"
			} else {
				item["tip"] = "暂无可操作"
			}
		}

		if _, blacklisted := blackMap[f.GID]; blacklisted {
			item["tip"] = "已拉黑"
			item["blacklisted"] = true
		}

		friends = append(friends, item)
	}

	// 按名称中文序、再 gid 排序
	sort.SliceStable(friends, func(i, j int) bool {
		ni, _ := friends[i]["name"].(string)
		nj, _ := friends[j]["name"].(string)
		if ni != nj {
			return ni < nj
		}
		gi, _ := friends[i]["uid"].(int64)
		gj, _ := friends[j]["uid"].(int64)
		return gi < gj
	})
	writeJSON(w, map[string]interface{}{"ok": true, "data": map[string]interface{}{
		"total":   len(friends),
		"friends": friends,
	}})
}

// countRipeLands 统计成熟且可偷（可收）的地块数
func countRipeLands(lands []*proto.LandInfo) int {
	n := 0
	for _, l := range lands {
		p := l.Plant
		if p == nil || len(p.Phases) == 0 {
			continue
		}
		cur := currentPhase(p.Phases, time.Now().Unix())
		if cur != nil && cur.Phase == proto.PhaseMature && p.Stealable {
			n++
		}
	}
	return n
}

// handleFriendLandsRoute GET /api/friends/lands?gid=xxx  好友地块明细（真实作物图）
// handleFriendListCacheClear 清空指定账号的好友列表展示缓存。
func handleFriendListCacheClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	clearFriendsListCache(accountID)
	writeJSON(w, map[string]interface{}{"ok": true, "message": "好友列表缓存已清"})
}

func handleFriendLandsRoute(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	gidStr := r.URL.Query().Get("gid")
	gid, err := strconv.ParseInt(gidStr, 10, 64)
	if gidStr == "" || err != nil || gid <= 0 {
		writeError(w, 400, "缺少有效 gid")
		return
	}
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	// 真实地块：进入好友农场解析，含真实作物图
	detail, derr := getFriendLandsForDisplay(c, gid)
	if derr != nil {
		writeError(w, 500, "获取好友地块失败: "+derr.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": detail})
}

func handleFriendBlacklist(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	// 本地黑名单库
	entries := getBlacklistEntries(accountID)
	out := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]interface{}{
			"uid":       e.GID,
			"name":      e.Name,
			"avatar":    "",
			"reason":    e.Reason,
			"addedAt":   e.AddedAt,
			"skipSteal": e.SkipSteal,
			"skipHelp":  e.SkipHelp,
		})
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": out})
}

func handleFriendRequests(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	rep, err := c.Request(r.Context(), friendService, "GetApplications",
		proto.EncodeGetApplicationsRequest(), 12*time.Second)
	if err != nil {
		writeError(w, 500, "拉取好友申请失败: "+err.Error())
		return
	}
	ap := proto.DecodeGetApplicationsReply(rep.Body)
	out := make([]map[string]interface{}, 0, len(ap.Applications))
	for _, a := range ap.Applications {
		out = append(out, map[string]interface{}{
			"gid":    a.GID,
			"name":   firstNonEmpty(a.Name, fmt.Sprintf("GID:%d", a.GID)),
			"avatar": a.AvatarURL,
			"level":  a.Level,
			"at":     a.TimeAt,
		})
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": out, "blocked": ap.BlockApplications})
}

func handleFriendVisitors(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	// 多服务路由候选，取首个成功
	var recs []*proto.InteractRecord
	var lastErr error
	for _, cand := range proto.InteractRecordCandidates {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		rep, err := c.Request(ctx, cand[0], cand[1], proto.EncodeInteractRecordsRequest(), 8*time.Second)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		recs = proto.DecodeInteractRecordsReply(rep.Body)
		if len(recs) > 0 {
			break
		}
	}
	if recs == nil {
		// 全部路由失败：返回空而非报错，同时给出诊断
		writeJSON(w, map[string]interface{}{"ok": true, "data": []interface{}{}, "errorHint": fmt.Sprint(lastErr)})
		return
	}

	// 时间降序 → 访客ID降序 → 操作类型降序
	sort.SliceStable(recs, func(i, j int) bool {
		if recs[i].ServerTime != recs[j].ServerTime {
			return recs[i].ServerTime > recs[j].ServerTime
		}
		if recs[i].VisitorGID != recs[j].VisitorGID {
			return recs[i].VisitorGID > recs[j].VisitorGID
		}
		return recs[i].ActionType > recs[j].ActionType
	})

	out := make([]map[string]interface{}, 0, len(recs))
	for i, rec := range recs {
		name := rec.Nick
		if name == "" {
			name = fmt.Sprintf("GID:%d", rec.VisitorGID)
		}
		out = append(out, map[string]interface{}{
			"key":          fmt.Sprintf("%d-%d-%d-%d", rec.ServerTime, rec.VisitorGID, rec.ActionType, i),
			"visitorGid":   rec.VisitorGID,
			"nick":         name,
			"avatarUrl":    rec.AvatarURL,
			"actionType":   rec.ActionType,
			"actionLabel":  interactActionLabel(rec.ActionType),
			"actionDetail": buildInteractDetail(rec),
			"serverTimeMs": serverTimeMs(rec.ServerTime),
			"level":        rec.Level,
			"landId":       rec.LandID,
			"times":        rec.Times,
			"name":         name,
			"avatar":       rec.AvatarURL,
			"action":       interactActionLabel(rec.ActionType),
			"time":         formatVisitorTime(rec.ServerTime),
		})

	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": out})
}

// interactActionLabel 
func interactActionLabel(t int32) string {
	switch t {
	case 1:
		return "偷取"
	case 2:
		return "帮忙"
	case 3:
		return "捣乱"
	default:
		return "互动"
	}
}

// buildInteractDetail 
func buildInteractDetail(rec *proto.InteractRecord) string {
	var parts []string
	switch rec.ActionType {
	case 1:
		if rec.CropCount > 0 {
			parts = append(parts, fmt.Sprintf("偷取作物 × %d", rec.CropCount))
		} else {
			parts = append(parts, "偷取作物")
		}
	case 2:
		if rec.Times > 0 {
			parts = append(parts, fmt.Sprintf("帮忙 %d 次", rec.Times))
		} else {
			parts = append(parts, "帮忙")
		}
	case 3:
		if rec.Times > 0 {
			parts = append(parts, fmt.Sprintf("捣乱 %d 次", rec.Times))
		} else {
			parts = append(parts, "捣乱")
		}
	default:
		if rec.Times > 0 {
			parts = append(parts, fmt.Sprintf("互动 %d 次", rec.Times))
		} else {
			parts = append(parts, "互动")
		}
	}
	if rec.LandID > 0 {
		parts = append(parts, fmt.Sprintf("地块 %d", rec.LandID))
	}
	return strings.Join(parts, " · ")
}

// serverTimeMs 服务器秒 -> 毫秒
func serverTimeMs(sec int64) int64 {
	if sec <= 0 {
		return 0
	}
	return sec * 1000
}

// formatVisitorTime 服务器时间(秒) → 可读时间
func formatVisitorTime(sec int64) string {
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).Format("01-02 15:04")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

var _ = models.GetAccounts

// handleFarmAction 统一处理农场操作：harvest/work/plant/upgrade/full/clear

// allLandIDs 拉取全部地块 ID（供全收/一键务农使用）
func allLandIDs(c *gw.Client, ctx context.Context) ([]int64, error) {
	rep, err := c.Request(ctx, "gamepb.plantpb.PlantService", "AllLands",
		proto.EncodeAllLandsRequest(0), 15*time.Second)
	if err != nil {
		return nil, err
	}
	all := proto.DecodeAllLandsReply(rep.Body)
	ids := make([]int64, 0, len(all.Lands))
	for _, l := range all.Lands {
		ids = append(ids, l.ID)
	}
	return ids, nil
}

func handleFarmAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		LandID string `json:"landId"`
		SeedID string `json:"seedId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	if req.Action == "" {
		writeError(w, 400, "missing action")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	detail := req.Action
	switch req.Action {
	case "full": // 全部收获（is_all=true + 传全部地块 ids）
		ids, err := allLandIDs(c, r.Context())
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		if err := execFarmOp(c, "Harvest", proto.EncodeHarvestRequest(ids, c.GID, true)); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		recordOperation(accountID, "harvest", int64(len(ids)))
		detail = fmt.Sprintf("全部收获 %d 块地", len(ids))
	case "harvest": // 收获：未指定地块则全部收获（is_all=true ）
		ids := parseIDs(req.LandID)
		if len(ids) == 0 {
			all, err := allLandIDs(c, r.Context())
			if err != nil {
				writeError(w, 500, err.Error())
				return
			}
			if err := execFarmOp(c, "Harvest", proto.EncodeHarvestRequest(all, c.GID, true)); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			recordOperation(accountID, "harvest", int64(len(all)))
			detail = "全部收获"
		} else {
			if err := execFarmOp(c, "Harvest", proto.EncodeHarvestRequest(ids, c.GID, true)); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			recordOperation(accountID, "harvest", int64(len(ids)))
			detail = fmt.Sprintf("收获 %d 块地", len(ids))
		}
	case "work": // 一键务农：未指定则对所有地块
		ids := parseIDs(req.LandID)
		if len(ids) == 0 {
			var err error
			ids, err = allLandIDs(c, r.Context())
			if err != nil {
				writeError(w, 500, err.Error())
				return
			}
		}
		if len(ids) == 0 {
			writeError(w, 400, "没有可操作地块")
			return
		}
		if err := execFarmOp(c, "Farming", proto.EncodeFarmingRequest(ids, c.GID)); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		recordOperation(accountID, "farming", int64(len(ids)))
		detail = fmt.Sprintf("一键务农 %d 块地", len(ids))
	case "upgrade": // 一键升级土地
		rep, err := c.Request(r.Context(), "gamepb.plantpb.PlantService", "AllLands",
			proto.EncodeAllLandsRequest(0), 15*time.Second)
		if err != nil {
			writeError(w, 500, "拉取农场失败: "+err.Error())
			return
		}
		all := proto.DecodeAllLandsReply(rep.Body)
		var unlockIDs, upgradeIDs []int64
		for _, l := range all.Lands {
			if !l.Unlocked && l.CouldUnlock {
				unlockIDs = append(unlockIDs, l.ID)
			}
			if l.CouldUpgrade {
				upgradeIDs = append(upgradeIDs, l.ID)
			}
		}
		unlocked, upgraded := 0, 0
		// 先解锁（unlockLand(landId, false)，逐个失败不中断）
		for _, id := range unlockIDs {
			if err := execFarmOp(c, "UnlockLand", proto.EncodeUnlockLandRequest(id, false)); err == nil {
				unlocked++
			}
			time.Sleep(200 * time.Millisecond)
		}
		// 再升级（upgradeLand(landId)，逐个失败不中断）
		for _, id := range upgradeIDs {
			if err := execFarmOp(c, "UpgradeLand", proto.EncodeUpgradeLandRequest(id)); err == nil {
				upgraded++
			}
			time.Sleep(200 * time.Millisecond)
		}
		if unlocked == 0 && upgraded == 0 {
			// 无候选时不报错（runFarmOperation 返回 hadWork:false，路由仍 ok:true）
			detail = "没有可解锁或可升级的土地"
			appendOpLog(accountID, req.Action, detail)
			writeJSON(w, map[string]interface{}{"ok": true, "action": req.Action, "message": detail})
			return
		}
		if upgraded > 0 {
			recordOperation(accountID, "upgrade", int64(upgraded))
		}
		detail = fmt.Sprintf("解锁 %d 块，升级 %d 块", unlocked, upgraded)
	case "clear": // 铲除（未指定地块则一键铲除全部）
		ids := parseIDs(req.LandID)
		if len(ids) == 0 {
			var err error
			ids, err = allLandIDs(c, r.Context())
			if err != nil {
				writeError(w, 500, err.Error())
				return
			}
		}
		if err := execFarmOp(c, "RemovePlant", proto.EncodeRemovePlantRequest(ids)); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		detail = fmt.Sprintf("铲除 %d 块地", len(ids))
	case "plant": // 手动种植：
		cfg := models.GetAccountConfig(accountID)
		if req.SeedID != "" && req.LandID != "" {
			seedID, perr := strconv.ParseInt(req.SeedID, 10, 64)
			if perr != nil || seedID <= 0 {
				writeError(w, 400, "missing or bad seedId")
				return
			}
			landIDs := parseIDs(req.LandID)
			if len(landIDs) == 0 {
				writeError(w, 400, "missing landId")
				return
			}
			n, perr := plantOnLands(accountID, c, seedID, landIDs)
			if perr != nil {
				writeError(w, 500, "种植失败: "+perr.Error())
				return
			}
			detail = fmt.Sprintf("种植 %d 块地", n)
		} else {
			// 未指定种子：用种植策略自动选种，种植当前农场所有空地/枯死地
			n, perr := autoPlantEmptyLands(accountID, c, cfg)
			if perr != nil {
				writeError(w, 500, "自动种植失败: "+perr.Error())
				return
			}
			detail = fmt.Sprintf("自动种植 %d 块地", n)
		}
	default:
		writeError(w, 400, "unknown action: "+req.Action)
		return
	}
	appendOpLog(accountID, req.Action, detail)
	writeJSON(w, map[string]interface{}{"ok": true, "action": req.Action, "message": detail})
}
