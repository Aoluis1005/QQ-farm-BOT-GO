package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ============================================================
// 图鉴接口
// 覆盖：
//   - GET  /api/illustrated            图鉴列表（refresh / illustrated_type）
//   - POST /api/illustrated/buy        购买单个种子（goodsId/price）
//   - POST /api/illustrated/buy-all    一键购买当前图鉴所有可购买项目（顺序执行，非并发 + 200ms 间隔）
// 协议：
//   - IllustratedService.GetIllustratedListV2
//   - ShopService.ShopInfo (shop_id=2 种子商店) / ShopService.BuyGoods
// ============================================================

const seedShopType = 2   // 种子商店
const buyAllDelayMs = 200 // 一键购买间隔

func registerIllustratedAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/illustrated", handleIllustratedGet)
	mux.HandleFunc("/api/illustrated/buy", handleIllustratedBuy)
	mux.HandleFunc("/api/illustrated/buy-all", handleIllustratedBuyAll)
}

// seedGoods 种子商店映射条目
type seedGoods struct {
	goodsID int64
	price   int64
}

// getUserLevel 用户等级（provider.getStatus(accountId)?.status?.level）
func getUserLevel(c *gw.Client) int64 {
	if c == nil {
		return 0
	}
	return c.Level()
}

// getSeedShopGoodsMap 拉种子商店商品，建立 item_id → {goodsId, price} 映射
func getSeedShopGoodsMap(ctx context.Context, c *gw.Client, tolerateFailure bool) map[int]seedGoods {
	m := map[int]seedGoods{}
	rep, err := c.Request(ctx, "gamepb.shoppb.ShopService", "ShopInfo",
		proto.EncodeShopInfoRequest(seedShopType), 15*time.Second)
	if err != nil {
		return m
	}
	_ = tolerateFailure
	shop := proto.DecodeShopInfoReply(rep.Body)
	for _, g := range shop.GoodsList {
		itemID := int(g.ItemID)
		if itemID > 0 && g.ID > 0 {
			m[itemID] = seedGoods{goodsID: g.ID, price: g.Price}
		}
	}
	return m
}

// resolveFruitDisplayName 果实显示名兜底
// 普通作物：fruitId = seedId + 20000，plantId = seedId + 1000000
// 变异作物：fruitId = 1040000 + n，plantId = 1120000 + n
func resolveFruitDisplayName(fruitID int) string {
	if fruitID <= 0 {
		return ""
	}
	if plant, ok := getPlantByFruitID(fruitID); ok && plant.Name != "" {
		return plant.Name
	}
	if fruitID > 1000000 {
		return getPlantNameOrNull(int64(fruitID - 1040000 + 1120000))
	}
	return getPlantNameOrNull(int64(fruitID - 20000 + 1000000))
}

// buildIllustratedItem 构造图鉴条目
func buildIllustratedItem(rawItem proto.IllustratedItem, seedGoodsMap map[int]seedGoods, userLevel int64) map[string]interface{} {
	fruitID := int(rawItem.SeedID)
	fruitConfig, _ := getItemByID(fruitID)
	seedID, seedLevel := getPlantSeedInfo(fruitID)
	unlocked := rawItem.Unlocked
	item := map[string]interface{}{}

	goodsID := int64(0)
	price := int64(0)
	if sg, ok := seedGoodsMap[seedID]; ok {
		goodsID = sg.goodsID
		price = sg.price
	}
	canBuy := !unlocked && seedID > 0 && seedLevel > 0 && userLevel >= int64(seedLevel) && goodsID > 0

	name := fruitConfig.Name
	if name == "" {
		name = resolveFruitDisplayName(fruitID)
	}
	if name == "" {
		name = "果实" + strconv.Itoa(fruitID)
	}

	item["seedId"] = fruitID
	item["unlocked"] = unlocked
	item["plantedCount"] = 0
	item["harvestCount"] = rawItem.HarvestCount
	item["name"] = name
	item["image"] = getSeedImageBySeedID(fruitID)
	item["level"] = fruitConfig.Level
	item["layer"] = getFruitLayerByFruitID(fruitID)
	item["canBuy"] = canBuy
	item["goodsId"] = goodsID
	item["price"] = price
	item["seedLevel"] = seedLevel
	return item
}

// summarizeIllustratedItems 图鉴汇总
func summarizeIllustratedItems(items []map[string]interface{}) map[string]interface{} {
	total := len(items)
	unlocked := 0
	canBuy := 0
	for _, it := range items {
		if b, _ := it["unlocked"].(bool); b {
			unlocked++
		}
		if b, _ := it["canBuy"].(bool); b {
			canBuy++
		}
	}
	return map[string]interface{}{
		"total":    total,
		"unlocked": unlocked,
		"locked":   total - unlocked,
		"canBuy":   canBuy,
	}
}

// collectBuyableIllustratedItems 收集可购买图鉴项目
func collectBuyableIllustratedItems(rawItems []proto.IllustratedItem, seedGoodsMap map[int]seedGoods, userLevel int64) []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, rawItem := range rawItems {
		fruitID := int(rawItem.SeedID)
		if rawItem.Unlocked {
			continue
		}
		seedID, seedLevel := getPlantSeedInfo(fruitID)
		if seedID <= 0 || seedLevel <= 0 || userLevel < int64(seedLevel) {
			continue
		}
		sg, ok := seedGoodsMap[seedID]
		if !ok {
			continue
		}
		name := getItemNameByID(fruitID)
		if name == "" {
			name = "果实" + strconv.Itoa(fruitID)
		}
		out = append(out, map[string]interface{}{
			"fruitId": fruitID,
			"seedId":  seedID,
			"goodsId": sg.goodsID,
			"price":   sg.price,
			"name":    name,
		})
	}
	return out
}

// getItemNameByID 物品名（?.name || `果实${id}`）
func getItemNameByID(itemID int) string {
	if it, ok := getItemByID(itemID); ok {
		return it.Name
	}
	return ""
}

// handleIllustratedGet GET /api/illustrated?refresh=&illustrated_type=
func handleIllustratedGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing x-account-id")
		return
	}
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}

	refresh := r.URL.Query().Get("refresh") == "true"
	illustratedType, _ := strconv.Atoi(r.URL.Query().Get("illustrated_type"))
	if illustratedType == 0 {
		illustratedType = 1
	}
	userLevel := getUserLevel(c)

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	seedGoodsMap := getSeedShopGoodsMap(ctx, c, true)
	listRep, err := c.Request(ctx, "gamepb.illustratedpb.IllustratedService", "GetIllustratedListV2",
		proto.EncodeGetIllustratedListV2Request(refresh, illustratedType), 20*time.Second)
	if err != nil {
		writeError(w, 500, "获取图鉴失败: "+err.Error())
		return
	}
	illustrated := proto.DecodeGetIllustratedListV2Reply(listRep.Body)

	items := make([]map[string]interface{}, 0, len(illustrated.Items))
	for _, it := range illustrated.Items {
		items = append(items, buildIllustratedItem(it, seedGoodsMap, userLevel))
	}
	summary := summarizeIllustratedItems(items)

	writeJSON(w, map[string]interface{}{
		"ok":   true,
		"data": map[string]interface{}{
			"items":     items,
			"summary":   summary,
			"userLevel": userLevel,
		},
	})
}

// handleIllustratedBuy POST /api/illustrated/buy {goodsId, price}
func handleIllustratedBuy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing x-account-id")
		return
	}
	var body struct {
		GoodsID int64 `json:"goodsId"`
		Price   int64 `json:"price"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.GoodsID == 0 {
		writeError(w, 400, "缺少商品ID")
		return
	}
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	_, err = c.Request(ctx, "gamepb.shoppb.ShopService", "BuyGoods",
		proto.EncodeBuyGoodsRequest(body.GoodsID, 1, body.Price), 15*time.Second)
	if err != nil {
		writeError(w, 500, "购买失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": map[string]interface{}{"ok": true}})
}

// handleIllustratedBuyAll POST /api/illustrated/buy-all {illustrated_type}
func handleIllustratedBuyAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing x-account-id")
		return
	}
	var body struct {
		IllustratedType int `json:"illustrated_type"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.IllustratedType == 0 {
		body.IllustratedType = 1
	}
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	userLevel := getUserLevel(c)

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	seedGoodsMap := getSeedShopGoodsMap(ctx, c, false)
	listRep, err := c.Request(ctx, "gamepb.illustratedpb.IllustratedService", "GetIllustratedListV2",
		proto.EncodeGetIllustratedListV2Request(false, body.IllustratedType), 20*time.Second)
	if err != nil {
		writeError(w, 500, "获取图鉴失败: "+err.Error())
		return
	}
	illustrated := proto.DecodeGetIllustratedListV2Reply(listRep.Body)
	buyableItems := collectBuyableIllustratedItems(illustrated.Items, seedGoodsMap, userLevel)

	type buyResult struct {
		FruitID int    `json:"fruitId"`
		SeedID  int    `json:"seedId"`
		Name    string `json:"name"`
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	results := []buyResult{}
	successCount := 0
	failCount := 0

	// for 顺序逐个购买 + 200ms 间隔（非并发）
	for _, item := range buyableItems {
		goodsID := item["goodsId"].(int64)
		price := item["price"].(int64)
		_, err := c.Request(ctx, "gamepb.shoppb.ShopService", "BuyGoods",
			proto.EncodeBuyGoodsRequest(goodsID, 1, price), 15*time.Second)
		res := buyResult{
			FruitID: item["fruitId"].(int),
			SeedID:  item["seedId"].(int),
			Name:    item["name"].(string),
		}
		if err == nil {
			successCount++
			res.Success = true
		} else {
			failCount++
			res.Error = err.Error()
		}
		results = append(results, res)
		if buyAllDelayMs > 0 {
			select {
			case <-time.After(time.Duration(buyAllDelayMs) * time.Millisecond):
			case <-ctx.Done():
			}
		}
	}

	writeJSON(w, map[string]interface{}{
		"ok": true,
		"data": map[string]interface{}{
			"total":        len(buyableItems),
			"successCount": successCount,
			"failCount":    failCount,
			"results":      results,
		},
	})
}
