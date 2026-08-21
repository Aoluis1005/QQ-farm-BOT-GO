package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/Aoluis1005/go-farm-bot/config"
	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/models"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ============================================================
// 设置接口
// 覆盖：自动控制(automation)、种植策略(strategy)、默认方案(default-plan)、种子列表(/api/seeds)
// 逻辑关系（Node 确认）：
//   - 自动控制 = 账号级开关集合（POST /api/automation）
//   - 种植策略 = 账号级种植选择（POST /api/settings/save）
//   - 默认方案 = 用户级模板，打包 策略+自动控制+间隔+化肥参数；新建账号 enabled 时自动套用
// ============================================================

func registerSettingsAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings", handleSettingsGet)
	mux.HandleFunc("/api/settings/save", handleSettingsSave)
	mux.HandleFunc("/api/automation", handleAutomationSave)
	mux.HandleFunc("/api/settings/default-plan", handleDefaultPlan)
	mux.HandleFunc("/api/settings/default-plan/import", handleDefaultPlanImport)
	mux.HandleFunc("/api/settings/default-plan/apply", handleDefaultPlanApply)
	mux.HandleFunc("/api/settings/default-plan/reset", handleDefaultPlanReset)
	mux.HandleFunc("/api/seeds", handleSeeds)
}

// reqAccountID 解析请求中的账号 ID：query accountId 优先，其次 x-account-id header
func reqAccountID(r *http.Request) string {
	id := r.URL.Query().Get("accountId")
	if id == "" {
		id = r.Header.Get("x-account-id")
	}
	return resolveAccountID(id)
}

// GET /api/settings  获取账号全量配置
func handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing x-account-id")
		return
	}
	cfg := models.GetAccountConfig(accountID)
	writeJSON(w, map[string]interface{}{"ok": true, "data": cfg})
}

// POST /api/settings/save  全量保存账号配置（除 automation；automation 走 /api/automation）
// ── 设置合法性钳制 ──
var allowedPlantingStrategies = map[string]bool{
	"preferred":       true,
	"level":           true,
	"max_exp":         true,
	"max_fert_exp":    true,
	"max_profit":      true,
	"max_fert_profit": true,
	"bag_priority":    true,
}

// clampPlantDelaySeconds  Math.max(0, Math.min(60, Number(x) || 2))
func clampPlantDelaySeconds(v int) int {
	if v == 0 {
		return 2
	}
	if v < 0 {
		return 0
	}
	if v > 60 {
		return 60
	}
	return v
}

// normalizePlantingConfig 把种植策略/延迟钳制到合法范围，非法策略归一到 Node 默认 max_exp。
func normalizePlantingConfig(cfg *config.AccountConfig) {
	if !allowedPlantingStrategies[cfg.PlantingStrategy] {
		cfg.PlantingStrategy = "max_exp"
	}
	cfg.PlantDelaySeconds = clampPlantDelaySeconds(cfg.PlantDelaySeconds)
}

func handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing x-account-id")
		return
	}
	cfg := models.GetAccountConfig(accountID) // 以现有为基底，只覆盖 body 中出现的字段
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, 400, "bad json: "+err.Error())
		return
	}
	normalizePlantingConfig(&cfg)
	if err := models.SetAccountConfig(accountID, cfg); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": models.GetAccountConfig(accountID)})
}

// POST /api/automation  保存自动化开关（全量对象）
func handleAutomationSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing x-account-id")
		return
	}
	var aut config.AutomationConfig
	if err := json.NewDecoder(r.Body).Decode(&aut); err != nil {
		writeError(w, 400, "bad json: "+err.Error())
		return
	}
	cfg := models.GetAccountConfig(accountID)
	cfg.Automation = aut
	if err := models.SetAccountConfig(accountID, cfg); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": models.GetAutomation(accountID)})
}

// GET/PUT /api/settings/default-plan  读/存默认方案
func handleDefaultPlan(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]interface{}{"ok": true, "data": models.GetUserDefaultPlan()})
	case http.MethodPut:
		var body struct {
			Enabled *bool                `json:"enabled"`
			Config  config.AccountConfig `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, 400, "bad json: "+err.Error())
			return
		}
		enabled := true
		if body.Enabled != nil {
			enabled = *body.Enabled
		}
		normalizePlantingConfig(&body.Config)
		if err := models.SetUserDefaultPlan(body.Config, enabled); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "data": models.GetUserDefaultPlan()})
	default:
		writeError(w, 405, "method not allowed")
	}
}

// POST /api/settings/default-plan/import  从当前账号导入为默认方案
func handleDefaultPlanImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing x-account-id")
		return
	}
	plan := models.GetUserDefaultPlan()
	cfg := models.GetAccountConfig(accountID)
	if err := models.SetUserDefaultPlan(cfg, plan.Enabled); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": models.GetUserDefaultPlan()})
}

// POST /api/settings/default-plan/apply  把默认方案套用到当前账号
func handleDefaultPlanApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing x-account-id")
		return
	}
	cfg, err := models.ApplyUserDefaultPlan(accountID)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": cfg})
}

// POST /api/settings/default-plan/reset  恢复系统默认
func handleDefaultPlanReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	if err := models.ResetUserDefaultPlan(); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": models.GetUserDefaultPlan()})
}

// seedOut 种子列表条目
// {seedId, goodsId:0, name, price:null, requiredLevel, locked:false, soldOut:false, unknownMeta:true}
type seedOut struct {
	SeedID        int    `json:"seedId"`
	GoodsID       int    `json:"goodsId"`
	Name          string `json:"name"`
	Price         *int   `json:"price"`
	RequiredLevel int    `json:"requiredLevel"`
	Locked        bool   `json:"locked"`
	SoldOut       bool   `json:"soldOut"`
	UnknownMeta   bool   `json:"unknownMeta"`
}

// GET /api/seeds  选种预览种子列表
// 主分支：有账号且在线时按账号实时拉商店可买种子（含 locked/soldOut 状态；活动种子商店不卖，天然不出现）；
// 回退分支：无账号/商店失败时回退本地全量种子（保留活动种子排除，避免优先下拉展示买不到的种子）。
func handleSeeds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	var out []seedOut
	if id := reqAccountID(r); id != "" {
		if c, err := clientPool.Get(id); err == nil {
			if list, ok := buildShopSeedList(c); ok {
				out = list
			}
		}
	}
	if len(out) == 0 {
		out = buildLocalSeedList()
	}
	// 按等级升序
	sort.Slice(out, func(i, j int) bool {
		if out[i].RequiredLevel != out[j].RequiredLevel {
			return out[i].RequiredLevel < out[j].RequiredLevel
		}
		return out[i].SeedID < out[j].SeedID
	})
	writeJSON(w, map[string]interface{}{"ok": true, "data": out})
}

// buildShopSeedList 按账号从商店构建可买种子列表（商店主分支）
func buildShopSeedList(c *gw.Client) ([]seedOut, bool) {
	rep, err := c.Request(context.Background(), "gamepb.shoppb.ShopService", "ShopInfo",
		proto.EncodeShopInfoRequest(2), 15*time.Second)
	if err != nil {
		return nil, false
	}
	level := c.Level()
	out := make([]seedOut, 0, 32)
	for _, g := range proto.DecodeShopInfoReply(rep.Body).GoodsList {
		if g.ItemID <= 0 {
			continue
		}
		reqLvl := 0
		for _, cd := range g.Conds {
			if cd.Type == 1 {
				reqLvl = int(cd.Param)
			}
		}
		price := int(g.Price)
		name := "种子" + strconv.Itoa(int(g.ItemID))
		if it, ok := itemInfoMap[int(g.ItemID)]; ok && it.Name != "" {
			name = it.Name
		} else if p, ok := seedToPlantMap[int(g.ItemID)]; ok && p.Name != "" {
			name = p.Name
		}
		out = append(out, seedOut{
			SeedID:        int(g.ItemID),
			GoodsID:       int(g.ID),
			Name:          name,
			Price:         &price,
			RequiredLevel: reqLvl,
			Locked:        !g.Unlocked || int(level) < reqLvl,
			SoldOut:       g.LimitCount > 0 && g.BoughtNum >= g.LimitCount,
			UnknownMeta:   false,
		})
	}
	return out, len(out) > 0
}

// buildLocalSeedList 本地全量种子列表（商店失败回退 getAllSeeds）
// 保留活动种子/黑名单排除，避免优先种子下拉展示商店买不到的种子。
func buildLocalSeedList() []seedOut {
	seedIDs := map[int]bool{}
	for id, it := range itemInfoMap {
		if it.Type == 5 && it.ID > 0 {
			seedIDs[id] = true
		}
	}
	for seedID := range seedToPlantMap {
		if seedID > 0 {
			seedIDs[seedID] = true
		}
	}
	out := make([]seedOut, 0, len(seedIDs))
	for id := range seedIDs {
		// 排除活动种子+动态黑名单（不应作为优先种子选项）
		if eventSeeds[int64(id)] || isBuySeedBlocked(int64(id)) {
			continue
		}
		item := seedOut{
			SeedID:        id,
			GoodsID:       0,
			Price:         nil,
			RequiredLevel: 0,
			Locked:        false,
			SoldOut:       false,
			UnknownMeta:   true,
		}
		// 名称：优先 ItemInfo type5 名（"草莓种子"）否则 Plant 植物名
		if it, ok := itemInfoMap[id]; ok && it.Name != "" {
			item.Name = it.Name
			item.RequiredLevel = it.Level
		} else if p, ok := seedToPlantMap[id]; ok {
			item.Name = p.Name
		}
		out = append(out, item)
	}
	return out
}
