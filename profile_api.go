package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	mux.HandleFunc("/api/bag/items", handleBagItems)
	mux.HandleFunc("/api/bag/use", handleBagUse)
	mux.HandleFunc("/api/bag/sell", handleBagSell)
	mux.HandleFunc("/api/friends/list", handleFriendList)
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
	now := time.Now().Unix()

	lands := make([]map[string]interface{}, 0, len(all.Lands))
	for _, l := range all.Lands {
		status, name, progress, timeLeft := analyzeLand(l, now)
		land := map[string]interface{}{
			"id":       l.ID,
			"state":    iconFor(name),
			"status":   status,
			"name":     name,
			"progress": progress,
			"timeLeft": timeLeft,
			"level":    l.Level,
			"fruit":    fruitCount(l.Plant),
		}
		// 接真实作物图：Plant(id=种子ID) → seed_images_named/{id}_xxx_Crop_N_Seed.png；无图保留 emoji 兜底
		if p := l.Plant; p != nil && p.ID > 0 {
			land["seedId"] = p.ID
			if img := GetItemImageURL(int(p.ID)); img != "" {
				land["img"] = img
			}
		}
		lands = append(lands, land)
	}

	writeJSON(w, map[string]interface{}{"ok": true, "data": lands})
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
	writeJSON(w, map[string]interface{}{"ok": true, "landId": landID, "message": "harvest ok"})
}

func handleFarmPlant(w http.ResponseWriter, r *http.Request) {
	landID := r.FormValue("landId")
	seedID := r.FormValue("seedId")
	if landID == "" || seedID == "" {
		writeError(w, 400, "missing landId or seedId")
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "landId": landID, "seedId": seedID, "message": "plant ok"})
}

func handleBagItems(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	// 真实背包数据：对齐 Node warehouse.js getBag → itempb.ItemService/Bag
	rep, err := c.Request(r.Context(), "gamepb.itempb.ItemService", "Bag",
		proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		writeError(w, 500, "拉取背包失败: "+err.Error())
		return
	}
	br := proto.DecodeBagReply(rep.Body)

	type bagOut struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Count    int64  `json:"count"`
		Category string `json:"category"`
		Img      string `json:"img,omitempty"`
		Icon     string `json:"icon,omitempty"`
		ItemType int64  `json:"itemType"` // 对齐 Node info.type：6/17=果实可售, 11=道具可用
		UID      int64  `json:"uid"`       // 物品实例 uid，出售时回传
	}
	items := make([]bagOut, 0, len(br.Items))
	for _, it := range br.Items {
		if it.ID <= 0 || it.Count <= 0 {
			continue
		}
		cat, name := classifyBagCategory(it.ID)
		outItem := bagOut{ID: it.ID, Name: name, Count: it.Count, Category: cat,
			ItemType: int64(itemInfoMap[int(it.ID)].Type), UID: it.UID}
		if img := GetItemImageURL(int(it.ID)); img != "" {
			outItem.Img = img
		} else {
			outItem.Icon = iconFor(name)
		}
		items = append(items, outItem)
	}

	// 排序：果实→种子→化肥→道具→其他，同类数量降序（对齐 Node getBagDetail 排序意图）
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

// handleBagUse POST /api/bag/use 对齐 Node admin-bag-routes.js POST /api/bag/use
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
	if _, err := c.Request(r.Context(), "gamepb.itempb.ItemService", "Use",
		proto.EncodeUseRequest(req.ItemID, req.Count), 12*time.Second); err != nil {
		writeError(w, 500, "使用失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "message": "use ok"})
}

// handleBagSell POST /api/bag/sell 对齐 Node admin-bag-routes.js POST /api/bag/sell
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
	if _, err := c.Request(r.Context(), "gamepb.itempb.ItemService", "Sell",
		proto.EncodeSellRequest(items), 12*time.Second); err != nil {
		writeError(w, 500, "出售失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "message": "sell ok"})
}

// classifyBagCategory 对齐 Node warehouse.js getBagDetail 的 category 判定。
// 前端分类 tab：fruit/seed/props(=props+fertilizer)/other。分类判定走 game_config.go（源于 Plant.json+ItemInfo.json）。
func classifyBagCategory(id int64) (category, name string) {
	switch id {
	case 1001, 500001:
		return "other", "金币"
	case 1002, 500002:
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

// handleFriendList 真实好友列表（对齐 Node friend-land-analyzer.js getFriendsList）：
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
	// 对齐 Node getAllFriends：微信直接 GetAll；QQ 用 GetGameFriends(已知GID) 回退 GetAll
	platform := ""
	if acc := models.GetAccountByID(accountID); acc != nil {
		platform = acc.Platform
	}
	knownGids := models.GetAccountConfig(accountID).KnownFriendGIDs
	allFriends, err := fetchAllFriends(c, platform, knownGids)
	if err != nil {
		writeError(w, 500, "拉取好友失败: "+err.Error())
		return
	}

	myGID := c.GID
	// 黑名单取自本地文件（与 toggle/blacklist tab 同源），对齐 Node getFriendBlacklistDetails
	blackMap := readBlacklist(accountID)
	// 护主犬缓存（对齐 Node readFriendDogInfoCache）
	dogMap, _ := readDogCache(accountID)

	friends := make([]map[string]interface{}, 0, len(allFriends))
	for _, f := range allFriends {
		if f.GID <= 0 || f.GID == myGID {
			continue
		}
		// 排除假 NPC（对齐 Node getFriendsList："小小农夫" level 1）
		if (f.Name == "小小农夫" || f.Remark == "小小农夫") && f.Level == 1 {
			continue
		}

		name := firstNonEmpty(f.Remark, f.Name, fmt.Sprintf("GID:%d", f.GID))
		item := map[string]interface{}{
			"uid":    f.GID,
			"gid":    f.GID,
			"name":   name,
			"avatar": f.AvatarURL,
			"level":  f.Level,
			"coins":  f.Gold,
			"hasDog": false,
			"dogId":  int64(0),
			"dogName": "",
			"canSteal": false,
			"canHelp":  false,
			"canBad":   true,
			"ripeLands": 0,
			"totalLands": 0,
			"tip": "",
		}

		// 护主犬：本地缓存优先（对齐 Node getFriendsList 的 dogInfoCache）
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

	// 按名称中文序、再 gid 排序（对齐 Node getFriendsList）
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
	// 真实地块：进入好友农场解析（对齐 Node getFriendLandsDetail），含真实作物图
	detail, derr := getFriendLandsForDisplay(c, gid)
	if derr != nil {
		writeError(w, 500, "获取好友地块失败: "+derr.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": detail})
}

func handleFriendBlacklist(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	// 本地黑名单库（对齐 Node getFriendBlacklist 前端展示 name/avatar/reason/addedAt + skip 开关）
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
	// 对齐 Node friend-api.js getApplications → FriendService/GetApplications
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
	// 对齐 Node interact.js getInteractRecords：多服务路由候选，取首个成功
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
		// 全部路由失败：返回空而非报错（对齐 Node 前端“暂无访客记录”），同时给出诊断
		writeJSON(w, map[string]interface{}{"ok": true, "data": []interface{}{}, "errorHint": fmt.Sprint(lastErr)})
		return
	}

	// 时间降序 → 访客ID降序 → 操作类型降序（对齐 Node sort）
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

// interactActionLabel 对齐 Node interact.js getActionLabel / ACTION_LABELS
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

// buildInteractDetail 对齐 Node interact.js buildActionDetail
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

// serverTimeMs 服务器秒 -> 毫秒（对齐 Node serverTimeMs = serverTimeSec*1000）
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
	case "full": // 全部收获（is_all=true + 传全部地块 ids，对齐 Node）
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
	case "harvest": // 收获：未指定地块则全部收获（is_all=true 对齐 Node）
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
			if err := execFarmOp(c, "Harvest", proto.EncodeHarvestRequest(ids, c.GID, false)); err != nil {
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
	case "upgrade": // 升级土地
		ids := parseIDs(req.LandID)
		if len(ids) == 0 {
			writeError(w, 400, "缺少 landId")
			return
		}
		if err := execFarmOp(c, "UpgradeLand", proto.EncodeUpgradeLandRequest(ids[0])); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		detail = fmt.Sprintf("升级土地 %d", ids[0])
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
	case "plant":
		writeError(w, 501, "种植功能暂未接入（需商店买种子）")
		return
	default:
		writeError(w, 400, "unknown action: "+req.Action)
		return
	}
	appendOpLog(accountID, req.Action, detail)
	writeJSON(w, map[string]interface{}{"ok": true, "action": req.Action, "message": detail})
}
