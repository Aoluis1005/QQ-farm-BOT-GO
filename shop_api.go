package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ============================================================
// 商城接口（对齐 Node admin-shop-routes.js + 各 admin-*-shop-routes.js + shop-purchase-routes.js）
// 覆盖：
//   - GET  /api/shop/seed            种子商店（ShopInfo shop_id=2）
//   - GET  /api/shop/pet             宠物商店（ShopInfo shop_id=3，白名单）
//   - GET  /api/shop/decoration      装扮商店（本地配置 2130/2131）
//   - GET  /api/shop/mall            道具商城（MallService.GetMallListBySlotType slot=1，白名单+Meta）
//   - POST /api/shop/buy             通用购买（seed/pet/decoration → ShopService.BuyGoods）
//   - POST /api/shop/mall/buy        道具购买（MallService.Purchase）
//   - GET  /api/shop/mystery         神秘商人（MysteryShopService.GetActiveNPC）
//   - POST /api/shop/mystery/buy     神秘购买（MysteryShopService.Buy）
//   - POST /api/shop/mystery/abandon 请离神秘商人（MysteryShopService.Abandon）
// ============================================================

const (
	petShopType      = 3    // 宠物商店 shop_id（Node PET_SHOP_TYPE=3）
	shopRequestTO    = 15 * time.Second
	mysteryRequestTO = 60 * time.Second // Node 神秘超时 60s
)

// ---- 宠物商店白名单 ----
var petItemIDs = map[int64]bool{90011: true, 90002: true, 90003: true}
var petItemOrder = map[int64]int{90011: 1, 90002: 2, 90003: 3}

// ---- 装扮商店白名单 ----
var decorationItemIDs = []int{2130, 2131}

// ---- 道具商城白名单 / 硬编码 Meta / 价格覆盖（对齐 Node admin-mall-routes.js） ----
var mallOrder = []int64{1002, 1003, 1006}
var mallPriceOverride = map[int64]int64{1002: 42, 1003: 34, 1006: 33}
var mallMeta = map[int64]struct {
	name   string
	images []string
	layout string
}{
	1002: {"10小时有机化肥", []string{"/game-config/seed_images_named/80011_organic_1.png", "/game-config/seed_images_named/80013_organic_8.png"}, "horizontal"},
	1003: {"10小时化肥", []string{"/game-config/seed_images_named/80001_ordinary_1.png", "/game-config/seed_images_named/80003_ordinary_8.png"}, "horizontal"},
	1006: {"狗粮礼包", []string{"/game-config/seed_images_named/90004_dog_food_1.png", "/game-config/seed_images_named/90005_dog_food_3.png", "/game-config/seed_images_named/90006_dog_food_5.png"}, "triangle"},
}

// ---- 神秘商人货币名（对齐 Node CURRENCY_NAMES） ----
var mysteryCurrencyNames = map[int64]string{1001: "金币", 1002: "点券", 1005: "金豆豆"}

func registerShopAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/shop/seed", handleShopSeed)
	mux.HandleFunc("/api/shop/pet", handleShopPet)
	mux.HandleFunc("/api/shop/decoration", handleShopDecoration)
	mux.HandleFunc("/api/shop/mall", handleShopMall)
	mux.HandleFunc("/api/shop/mall/buy", handleShopMallBuy)
	mux.HandleFunc("/api/shop/buy", handleShopBuy)
	mux.HandleFunc("/api/shop/mystery", handleShopMystery)
	mux.HandleFunc("/api/shop/mystery/buy", handleShopMysteryBuy)
	mux.HandleFunc("/api/shop/mystery/abandon", handleShopMysteryAbandon)
}

// shopClient 解析账号ID并返回其客户端；未连接返回 nil
func shopClient(r *http.Request) *gw.Client {
	accountID := reqAccountID(r)
	if accountID == "" {
		return nil
	}
	c := clientPool.cached(accountID)
	if c == nil || c.IsClosed() {
		return nil
	}
	return c
}

// getSeedHarvestInfo 对齐 Node getSeedHarvestInfo：exp=plant.exp, seasons=plant.seasons, income=果实单价×果实数
func getSeedHarvestInfo(seedID int) (exp, seasons, income int64) {
	p, ok := seedToPlantMap[seedID]
	if !ok {
		return 0, 1, 0
	}
	exp = int64(p.Exp)
	seasons = int64(p.Seasons)
	if p.Fruit != nil {
		fp := int64(0)
		if it, ok2 := itemInfoMap[p.Fruit.ID]; ok2 {
			fp = int64(it.Price)
		}
		income = fp * int64(p.Fruit.Count)
	}
	return
}

// requiredLevelFromConds 从 conds 取 type=1 的等级条件
func requiredLevelFromConds(conds []proto.GoodsCond) int64 {
	for _, c := range conds {
		if c.Type == 1 {
			return c.Param
		}
	}
	return 0
}

// buildSeedGoods 构造种子商品（对齐 Node admin-seed-shop-routes；种子需 itemConfig.type===5）
func buildSeedGoods(g proto.GoodsInfo, userLevel int64) (map[string]interface{}, bool) {
	itemID := int(g.ItemID)
	itemConfig, ok := getItemByID(itemID)
	if !ok || itemConfig.Type != 5 {
		return nil, false
	}
	requiredLevel := requiredLevelFromConds(g.Conds)
	limitCount := g.LimitCount
	boughtNum := g.BoughtNum
	isSoldOut := limitCount > 0 && boughtNum >= limitCount
	exp, seasons, income := getSeedHarvestInfo(itemID)
	assetName := itemConfig.AssetName
	if assetName == "" {
		assetName = fmt.Sprintf("Crop_%d", itemID-20000)
	}
	name := itemConfig.Name
	if name == "" {
		name = "种子" + strconv.Itoa(itemID)
	}
	return map[string]interface{}{
		"id":              g.ID,
		"itemId":          itemID,
		"itemCount":       g.Count,
		"price":           g.Price,
		"limitCount":      limitCount,
		"boughtNum":       boughtNum,
		"unlocked":        g.Unlocked,
		"requiredLevel":   requiredLevel,
		"seedLevel":       itemConfig.Level,
		"name":            name,
		"assetName":       assetName,
		"image":           GetItemImageURL(itemID),
		"canBuy":          g.Unlocked && userLevel >= requiredLevel && !isSoldOut,
		"isSoldOut":       isSoldOut,
		"expPerSeason":    exp,
		"seasons":         seasons,
		"incomePerSeason": income,
	}, true
}

// buildPetGoods 构造宠物商品（对齐 Node admin-pet-shop-routes；白名单过滤）
func buildPetGoods(g proto.GoodsInfo, userLevel, userGold, userGoldBean int64) (map[string]interface{}, bool) {
	itemID := g.ItemID
	if !petItemIDs[itemID] {
		return nil, false
	}
	itemConfig, ok := getItemByID(int(itemID))
	if !ok {
		return nil, false
	}
	requiredLevel := requiredLevelFromConds(g.Conds)
	limitCount := g.LimitCount
	boughtNum := g.BoughtNum
	isSoldOut := limitCount > 0 && boughtNum >= limitCount
	price := g.Price
	isGoldenBean := itemID == 90011
	afford := userGold >= price
	if isGoldenBean {
		afford = userGoldBean >= price
	}
	name := itemConfig.Name
	if name == "" {
		name = "宠物" + strconv.FormatInt(itemID, 10)
	}
	return map[string]interface{}{
		"id":            g.ID,
		"itemId":        itemID,
		"itemCount":     g.Count,
		"price":         price,
		"limitCount":    limitCount,
		"boughtNum":     boughtNum,
		"unlocked":      g.Unlocked,
		"requiredLevel": requiredLevel,
		"name":          name,
		"image":         GetItemImageURL(int(itemID)),
		"desc":          "",
		"isGoldenBean":  isGoldenBean,
		"canBuy":        g.Unlocked && userLevel >= requiredLevel && !isSoldOut && afford,
		"isSoldOut":     isSoldOut,
	}, true
}

// buildDecorationItem 构造装扮商品（对齐 Node buildDecorationItem；价格取 itemConfig.price 金豆豆，id=itemId）
func buildDecorationItem(itemID int, userGoldBean int64) (map[string]interface{}, bool) {
	itemConfig, ok := getItemByID(itemID)
	if !ok {
		return nil, false
	}
	price := int64(itemConfig.Price)
	name := itemConfig.Name
	if name == "" {
		name = "装扮" + strconv.Itoa(itemID)
	}
	return map[string]interface{}{
		"id":         itemID,
		"itemId":     itemID,
		"itemCount":  1,
		"price":      price,
		"name":       name,
		"image":      GetItemImageURL(itemID),
		"desc":       "",
		"effectDesc": "",
		"canBuy":     userGoldBean >= price,
	}, true
}

// buildMallGoods 构造道具商城商品（对齐 Node admin-mall-routes；白名单+Meta，price 取 override 或解析 bytes）
func buildMallGoods(m proto.MallGoods, userTicket int64) (map[string]interface{}, bool) {
	id := m.GoodsID
	if _, inOrder := mallPriceOverride[id]; !inOrder {
		return nil, false
	}
	meta, ok := mallMeta[id]
	if !ok {
		return nil, false
	}
	isFree := m.IsFree
	isLimited := m.IsLimited
	price := mallPriceOverride[id]
	if price == 0 {
		price = proto.ParseMallPriceValue(m.PriceBytes)
	}
	var limitCount, boughtNum int64
	if isLimited && len(m.LimitBytes) > 0 {
		limitCount, boughtNum = proto.ParseMallLimitInfo(m.LimitBytes)
	}
	isSoldOut := limitCount > 0 && boughtNum >= limitCount
	return map[string]interface{}{
		"id":         id,
		"goodsId":    id,
		"name":       meta.name,
		"type":       m.Type,
		"itemIds":    []interface{}{},
		"price":      price,
		"isFree":     isFree,
		"isLimited":  isLimited,
		"limitCount": limitCount,
		"boughtNum":  boughtNum,
		"isSoldOut":  isSoldOut,
		"discount":   m.Discount,
		"images":     meta.images,
		"layout":     meta.layout,
		"canBuy":     !isSoldOut && (isFree || userTicket >= price),
	}, true
}

// normalizeNPC 对齐 Node normalizeNPC（神秘商人状态归一化，秒级时间戳）
func normalizeNPC(reply *proto.GetActiveNPCReply) map[string]interface{} {
	var npcID, itemID, itemType, itemCount, currencyID, price, originalPrice, discount int64
	purchased := false
	if reply.NPC != nil {
		npcID = reply.NPC.NpcID
		itemID = reply.NPC.ItemID
		itemType = reply.NPC.ItemType
		itemCount = reply.NPC.ItemCount
		currencyID = reply.NPC.CurrencyID
		price = reply.NPC.Price
		originalPrice = reply.NPC.OriginalPrice
		discount = reply.NPC.Discount
		purchased = reply.NPC.Purchased
	}
	nowMs := time.Now().UnixMilli()
	endTime := reply.EndTime
	active := reply.Active && !purchased && (endTime == 0 || endTime*1000 > nowMs)

	itemName := ""
	itemImage := ""
	if itemID > 0 {
		if it, ok := getItemByID(int(itemID)); ok && it.Name != "" {
			itemName = it.Name
		}
		itemImage = GetItemImageURL(int(itemID))
	}
	if itemName == "" {
		itemName = "物品" + strconv.FormatInt(itemID, 10)
	}
	currencyName := "货币" + strconv.FormatInt(currencyID, 10)
	if n, ok := mysteryCurrencyNames[currencyID]; ok {
		currencyName = n
	}
	return map[string]interface{}{
		"active":        active,
		"npcId":         npcID,
		"itemId":        itemID,
		"itemType":      itemType,
		"itemName":      itemName,
		"itemImage":     itemImage,
		"itemCount":     itemCount,
		"currencyId":    currencyID,
		"currencyName":  currencyName,
		"price":         price,
		"originalPrice": originalPrice,
		"discount":      discount,
		"purchased":     purchased,
		"startTime":     reply.StartTime,
		"endTime":       endTime,
	}
}

// ---- GET /api/shop/seed ----
func handleShopSeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing x-account-id")
		return
	}
	c := clientPool.cached(accountID)
	if c == nil || c.IsClosed() {
		writeJSON(w, map[string]interface{}{"ok": true, "data": []interface{}{}})
		return
	}
	userLevel := c.Level()
	rep, err := c.Request(r.Context(), "gamepb.shoppb.ShopService", "ShopInfo",
		proto.EncodeShopInfoRequest(seedShopType), shopRequestTO)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": true, "data": []interface{}{}})
		return
	}
	shop := proto.DecodeShopInfoReply(rep.Body)
	seeds := make([]map[string]interface{}, 0, len(shop.GoodsList))
	for _, g := range shop.GoodsList {
		if it, ok := buildSeedGoods(g, userLevel); ok {
			seeds = append(seeds, it)
		}
	}
	sort.Slice(seeds, func(i, j int) bool {
		return seedInt(seeds[i]["requiredLevel"]) < seedInt(seeds[j]["requiredLevel"])
	})
	writeJSON(w, map[string]interface{}{"ok": true, "data": seeds})
}

// ---- GET /api/shop/pet ----
func handleShopPet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := reqAccountID(r)
	if accountID == "" {
		writeError(w, 400, "Missing x-account-id")
		return
	}
	c := clientPool.cached(accountID)
	var userLevel, userGold, userGoldBean int64
	goods := []map[string]interface{}{}
	if c != nil && !c.IsClosed() {
		userLevel, userGold, userGoldBean = c.Level(), c.Gold(), c.GoldBean()
		rep, err := c.Request(r.Context(), "gamepb.shoppb.ShopService", "ShopInfo",
			proto.EncodeShopInfoRequest(petShopType), shopRequestTO)
		if err == nil {
			shop := proto.DecodeShopInfoReply(rep.Body)
			for _, g := range shop.GoodsList {
				if it, ok := buildPetGoods(g, userLevel, userGold, userGoldBean); ok {
					goods = append(goods, it)
				}
			}
		}
	}
	sort.Slice(goods, func(i, j int) bool {
		a, b := 99, 99
		if v, ok := petItemOrder[int64(seedInt(goods[i]["itemId"]))]; ok {
			a = v
		}
		if v, ok := petItemOrder[int64(seedInt(goods[j]["itemId"]))]; ok {
			b = v
		}
		return a < b
	})
	writeJSON(w, map[string]interface{}{"ok": true, "data": goods, "userGold": userGold, "userGoldBean": userGoldBean})
}

// ---- GET /api/shop/decoration ----
func handleShopDecoration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	c := shopClient(r)
	if c == nil {
		writeError(w, 400, "获取装扮商城失败: 账号未运行")
		return
	}
	userGoldBean := c.GoldBean()
	goods := []map[string]interface{}{}
	for _, id := range decorationItemIDs {
		if it, ok := buildDecorationItem(id, userGoldBean); ok {
			goods = append(goods, it)
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": goods, "userGoldBean": userGoldBean})
}

// ---- GET /api/shop/mall ----
func handleShopMall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	c := shopClient(r)
	if c == nil {
		writeError(w, 400, "获取道具商城失败: 账号未运行")
		return
	}
	userTicket := c.Coupon()
	// 对齐 Node：worker getMallGoods→getMallGoodsList(0)，但 mall.js 内部 slot_type: Number(slotType)||1，0→1，
	// 故商城 Tab 实际发送 slot_type=1 而非 0。
	rep, err := c.Request(r.Context(), "gamepb.mallpb.MallService", "GetMallListBySlotType",
		proto.EncodeGetMallListBySlotTypeRequest(1), shopRequestTO)
	if err != nil {
		writeError(w, 500, "获取道具商城失败: "+err.Error())
		return
	}
	list := proto.DecodeMallListBySlotTypeReply(rep.Body)
	goods := make([]map[string]interface{}, 0, len(list.GoodsList))
	for _, g := range list.GoodsList {
		if it, ok := buildMallGoods(g, userTicket); ok {
			goods = append(goods, it)
		}
	}
	sort.Slice(goods, func(i, j int) bool {
		return mallIndex(goods[i]["goodsId"]) < mallIndex(goods[j]["goodsId"])
	})
	writeJSON(w, map[string]interface{}{"ok": true, "data": goods, "userTicket": userTicket})
}

// ---- GET /api/shop/mystery ----
func handleShopMystery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	c := shopClient(r)
	if c == nil {
		writeError(w, 400, "获取神秘商人失败: 账号未运行")
		return
	}
	rep, err := c.Request(r.Context(), "gamepb.mysteryshoppb.MysteryShopService", "GetActiveNPC",
		proto.EncodeGetActiveNPCRequest(), mysteryRequestTO)
	if err != nil {
		writeError(w, 500, "获取神秘商人失败: "+err.Error())
		return
	}
	reply := proto.DecodeGetActiveNPCReply(rep.Body)
	writeJSON(w, map[string]interface{}{"ok": true, "data": normalizeNPC(reply)})
}

// ---- POST /api/shop/buy （seed/pet/decoration 通用） ----
func handleShopBuy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	c := shopClient(r)
	if c == nil {
		writeError(w, 400, "账号未运行")
		return
	}
	var body struct {
		GoodsID int64 `json:"goodsId"`
		Num     int64 `json:"num"`
		Price   int64 `json:"price"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad json: "+err.Error())
		return
	}
	if body.GoodsID <= 0 || body.Num <= 0 || body.Price == 0 {
		// price 可能为 0 表示免费？Node 判 price===undefined 才 400；这里仅当 goodsId/num 缺失（Node: price===undefined）
		if body.GoodsID <= 0 || body.Num <= 0 {
			writeError(w, 400, "参数不完整")
			return
		}
	}
	rep, err := c.Request(r.Context(), "gamepb.shoppb.ShopService", "BuyGoods",
		proto.EncodeBuyGoodsRequest(body.GoodsID, body.Num, body.Price), 15*time.Second)
	if err != nil {
		writeError(w, 500, "购买失败: "+err.Error())
		return
	}
	br := proto.DecodeBuyGoodsReply(rep.Body)
	writeJSON(w, map[string]interface{}{
		"ok": true,
		"data": map[string]interface{}{
			"goods":     br.Goods,
			"getItems":  itemChanges(br.GetItems),
			"costItems": itemChanges(br.CostItems),
		},
	})
}

// ---- POST /api/shop/mall/buy ----
func handleShopMallBuy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	c := shopClient(r)
	if c == nil {
		writeError(w, 400, "账号未运行")
		return
	}
	var body struct {
		GoodsID int64 `json:"goodsId"`
		Count   int64 `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad json: "+err.Error())
		return
	}
	if body.GoodsID <= 0 || body.Count <= 0 {
		writeError(w, 400, "参数不完整")
		return
	}
	rep, err := c.Request(r.Context(), "gamepb.mallpb.MallService", "Purchase",
		proto.EncodePurchaseRequest(body.GoodsID, body.Count), 15*time.Second)
	if err != nil {
		writeError(w, 500, "兑换失败: "+err.Error())
		return
	}
	pr := proto.DecodePurchaseReply(rep.Body)
	writeJSON(w, map[string]interface{}{"ok": true, "data": map[string]interface{}{"goodsId": pr.GoodsID, "count": pr.Count}})
}

// ---- POST /api/shop/mystery/buy ----
func handleShopMysteryBuy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	c := shopClient(r)
	if c == nil {
		writeError(w, 400, "账号未运行")
		return
	}
	var body struct {
		NpcID int64 `json:"npcId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad json: "+err.Error())
		return
	}
	if body.NpcID <= 0 {
		writeError(w, 400, "缺少神秘商人 ID")
		return
	}
	rep, err := c.Request(r.Context(), "gamepb.mysteryshoppb.MysteryShopService", "Buy",
		proto.EncodeMysteryBuyRequest(body.NpcID), 15*time.Second)
	if err != nil {
		writeError(w, 500, "购买失败: "+err.Error())
		return
	}
	br := proto.DecodeMysteryBuyReply(rep.Body)
	data := map[string]interface{}{"purchased": false}
	if br.NPC != nil {
		data["purchased"] = br.NPC.Purchased
	}
	if br.Reward != nil {
		data["reward"] = map[string]interface{}{"itemId": br.Reward.ItemID, "count": br.Reward.Count}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": data})
}

// ---- POST /api/shop/mystery/abandon ----
func handleShopMysteryAbandon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	c := shopClient(r)
	if c == nil {
		writeError(w, 400, "账号未运行")
		return
	}
	_, err := c.Request(r.Context(), "gamepb.mysteryshoppb.MysteryShopService", "Abandon",
		proto.EncodeMysteryAbandonRequest(), 15*time.Second)
	if err != nil {
		writeError(w, 500, "请离失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": map[string]interface{}{"abandoned": true}})
}

// ---- 小工具 ----
func seedInt(v interface{}) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	}
	return 0
}

func mallIndex(v interface{}) int {
	id := seedInt(v)
	for i, g := range mallOrder {
		if g == id {
			return i
		}
	}
	return 99
}

func itemChanges(items []proto.ItemChange) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]interface{}{"id": it.ID, "count": it.Count})
	}
	return out
}
