package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/Aoluis1005/go-farm-bot/models"
)

// ============================================================
// 分析接口（对齐 Node admin-analytics-routes.js + services/analytics.js）
//   - GET  /api/analytics?sort=exp|fert|gold|profit|fert_profit|level  植物排名
// 偷菜植物黑名单接口（对齐 Node admin-plant-blacklist-routes.js，配置存于 accountConfig.plantBlacklist）
//   - GET    /api/plant-blacklist
//   - POST   /api/plant-blacklist         {seedId}
//   - DELETE /api/plant-blacklist/:seedId
//   - POST   /api/plant-blacklist/batch   {seedIds:[]}
//   - DELETE /api/plant-blacklist
// ============================================================

const secondsPerHour = 3600

func registerAnalyticsAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/analytics", handleAnalytics)
	mux.HandleFunc("/api/plant-blacklist", handlePlantBlacklist)
	mux.HandleFunc("/api/plant-blacklist/batch", handlePlantBlacklistBatch)
	mux.HandleFunc("/api/plant-blacklist/", handlePlantBlacklistItem)
}

// ---- 解析（对齐 analytics.js） ----

// parseGrowTime 解析 grow_phases 总秒数（":(\d+)$" 每段求和）
func parseGrowTime(growPhases string) float64 {
	total := 0.0
	for _, phase := range strings.Split(growPhases, ";") {
		if phase == "" {
			continue
		}
		if idx := strings.LastIndex(phase, ":"); idx >= 0 {
			if sec, err := strconv.ParseFloat(phase[idx+1:], 64); err == nil {
				total += sec
			}
		}
	}
	return total
}

// parseNormalFertilizerReduceSec 普通化肥减少秒数（第一段时长）
func parseNormalFertilizerReduceSec(growPhases string) float64 {
	phases := strings.Split(growPhases, ";")
	for _, phase := range phases {
		if phase == "" {
			continue
		}
		if idx := strings.LastIndex(phase, ":"); idx >= 0 {
			if sec, err := strconv.ParseFloat(phase[idx+1:], 64); err == nil {
				return sec
			}
			return 0
		}
	}
	return 0
}

// formatGrowTimeSec 秒数人类可读（对齐 analytics.js formatTime）
func formatGrowTimeSec(secs float64) string {
	if secs < 60 {
		return fmt.Sprintf("%.0f秒", secs)
	}
	if secs < 3600 {
		mins := math.Floor(secs / 60)
		rem := int(secs) % 60
		if rem > 0 {
			return fmt.Sprintf("%.0f分%d秒", mins, rem)
		}
		return fmt.Sprintf("%.0f分", mins)
	}
	hours := math.Floor(secs / 3600)
	mins := math.Floor(math.Mod(secs, 3600) / 60)
	if mins > 0 {
		return fmt.Sprintf("%.0f时%.0f分", hours, mins)
	}
	return fmt.Sprintf("%.0f时", hours)
}

// round2 保留两位小数（对齐 JS Number.parseFloat(x.toFixed(2))）
func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

// getPlantRankings 植物排名（对齐 Node getPlantRankings）
func getPlantRankings(sortBy string) []map[string]interface{} {
	rankings := []map[string]interface{}{}
	for _, plant := range getAllPlants() {
		if plant.SeedID <= 0 || plant.GrowPhases == "" {
			continue
		}
		if eventSeeds[int64(plant.SeedID)] {
			continue // 活动种子排除（商店买不到，推荐/选种都不出现）
		}
		if isBuySeedBlocked(int64(plant.SeedID)) {
			continue // 动态黑名单：历史上购买失败的种子
		}
		growTime := parseGrowTime(plant.GrowPhases)
		if growTime <= 0 {
			continue
		}
		seasons := plant.Seasons
		if seasons == 0 {
			seasons = 1
		}
		isMultiSeason := seasons == 2
		effectiveGrowTime := growTime
		if isMultiSeason {
			effectiveGrowTime = growTime * 1.5
		}

		expPerHarvest := float64(plant.Exp)
		totalExp := expPerHarvest
		if isMultiSeason {
			totalExp = expPerHarvest * 2
		}

		expPerHour := totalExp / effectiveGrowTime * secondsPerHour

		reduceSec := parseNormalFertilizerReduceSec(plant.GrowPhases)
		reduceSecApplied := reduceSec
		if isMultiSeason {
			reduceSecApplied = reduceSec * 1.5
		}
		fertGrowTime := effectiveGrowTime - reduceSecApplied
		fertEffectiveTime := effectiveGrowTime
		if fertGrowTime > 0 {
			fertEffectiveTime = fertGrowTime
		}

		normalFertilizerExpPerHour := totalExp / fertEffectiveTime * secondsPerHour

		fruitID := 0
		fruitCountPerHarvest := 0.0
		if plant.Fruit != nil {
			fruitID = plant.Fruit.ID
			fruitCountPerHarvest = float64(plant.Fruit.Count)
		}
		fruitPrice := getFruitPrice(fruitID)
		seedPrice := getSeedPrice(plant.SeedID)

		income := fruitCountPerHarvest * fruitPrice
		if isMultiSeason {
			income *= 2
		}
		netProfit := income - seedPrice

		goldPerHour := income / effectiveGrowTime * secondsPerHour
		profitPerHour := netProfit / effectiveGrowTime * secondsPerHour
		fertProfitPerHour := netProfit / fertEffectiveTime * secondsPerHour

		level := getSeedLevel(plant.SeedID)
		var levelOrNull interface{}
		if level > 0 {
			levelOrNull = level
		}

		rankings = append(rankings, map[string]interface{}{
			"id":                               plant.ID,
			"seedId":                           plant.SeedID,
			"name":                             plant.Name,
			"seasons":                          seasons,
			"level":                            levelOrNull,
			"growTime":                         effectiveGrowTime,
			"growTimeStr":                      formatGrowTimeSec(effectiveGrowTime),
			"reduceSec":                        reduceSec,
			"reduceSecApplied":                 reduceSecApplied,
			"expPerHour":                       round2(expPerHour),
			"normalFertilizerExpPerHour":       round2(normalFertilizerExpPerHour),
			"goldPerHour":                      round2(goldPerHour),
			"profitPerHour":                    round2(profitPerHour),
			"normalFertilizerProfitPerHour":    round2(fertProfitPerHour),
			"income":                           income,
			"netProfit":                        netProfit,
			"fruitId":                          fruitID,
			"fruitCount":                       fruitCountPerHarvest,
			"fruitPrice":                       fruitPrice,
			"seedPrice":                        seedPrice,
			"image":                            getItemImageByID(plant.SeedID),
		})
	}

	// 排序（对齐 Node analytics.js）
	sortDesc := func(key string) {
		// 稳定地按数值降序
		sortRankings(rankings, key)
	}
	switch sortBy {
	case "fertilizer_exp", "fert", "fert_exp", "normal_fertilizer_exp":
		sortDesc("normalFertilizerExpPerHour")
	case "gold":
		sortDesc("goldPerHour")
	case "profit":
		sortDesc("profitPerHour")
	case "fert_profit", "fertilizer_profit", "normal_fertilizer_profit":
		sortDesc("normalFertilizerProfitPerHour")
	case "level":
		sortRankingsByLevel(rankings)
	default: // "exp"
		sortDesc("expPerHour")
	}
	return rankings
}

// sortRankings 按数值字段降序（null/缺失视为 -Infinity）
func sortRankings(list []map[string]interface{}, key string) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0; j-- {
			if numVal(list[j-1][key]) < numVal(list[j][key]) {
				list[j-1], list[j] = list[j], list[j-1]
			} else {
				break
			}
		}
	}
}

// sortRankingsByLevel 按 level 降序（null 视为 -Infinity）
func sortRankingsByLevel(list []map[string]interface{}) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0; j-- {
			if levelVal(list[j-1]["level"]) < levelVal(list[j]["level"]) {
				list[j-1], list[j] = list[j], list[j-1]
			} else {
				break
			}
		}
	}
}

func numVal(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return math.Inf(-1)
	}
}

func levelVal(v interface{}) float64 {
	if v == nil {
		return math.Inf(-1)
	}
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	default:
		return math.Inf(-1)
	}
}

// handleAnalytics GET /api/analytics?sort=
func handleAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "exp"
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": getPlantRankings(sortBy)})
}

// ---- 偷菜植物黑名单（对齐 Node admin-plant-blacklist-routes.js） ----

func getPlantBlacklist(accountID string) []int {
	cfg := models.GetAccountConfig(accountID)
	out := cfg.PlantBlacklist
	if out == nil {
		out = []int{}
	}
	return out
}

func setPlantBlacklist(accountID string, list []int) {
	cfg := models.GetAccountConfig(accountID)
	cfg.PlantBlacklist = list
	_ = models.SetAccountConfig(accountID, cfg)
}

func handlePlantBlacklist(w http.ResponseWriter, r *http.Request) {
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing accountId")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]interface{}{"ok": true, "data": getPlantBlacklist(accountID)})
	case http.MethodPost:
		var body struct {
			SeedID int `json:"seedId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.SeedID == 0 {
			writeError(w, 400, "Missing seedId")
			return
		}
		list := getPlantBlacklist(accountID)
		found := false
		for _, s := range list {
			if s == body.SeedID {
				found = true
				break
			}
		}
		if !found {
			list = append(list, body.SeedID)
			setPlantBlacklist(accountID, list)
		}
		writeJSON(w, map[string]interface{}{"ok": true, "data": getPlantBlacklist(accountID)})
	case http.MethodDelete:
		// DELETE /api/plant-blacklist（清空）
		setPlantBlacklist(accountID, []int{})
		writeJSON(w, map[string]interface{}{"ok": true, "data": []int{}})
	default:
		writeError(w, 405, "method not allowed")
	}
}

// handlePlantBlacklistBatch POST /api/plant-blacklist/batch {seedIds}
func handlePlantBlacklistBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing accountId")
		return
	}
	var body struct {
		SeedIDs []int `json:"seedIds"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	seen := map[int]bool{}
	list := getPlantBlacklist(accountID)
	for _, s := range list {
		seen[s] = true
	}
	next := list
	for _, s := range body.SeedIDs {
		if s > 0 && !seen[s] {
			next = append(next, s)
			seen[s] = true
		}
	}
	setPlantBlacklist(accountID, next)
	writeJSON(w, map[string]interface{}{"ok": true, "data": next})
}

// handlePlantBlacklistItem DELETE /api/plant-blacklist/:seedId
func handlePlantBlacklistItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing accountId")
		return
	}
	trimmed := strings.TrimSuffix(r.URL.Path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		writeError(w, 400, "Missing seedId")
		return
	}
	seedID, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || seedID == 0 {
		writeError(w, 400, "Missing seedId")
		return
	}
	list := getPlantBlacklist(accountID)
	next := []int{}
	for _, s := range list {
		if s != seedID {
			next = append(next, s)
		}
	}
	setPlantBlacklist(accountID, next)
	writeJSON(w, map[string]interface{}{"ok": true, "data": next})
}
