package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ===== 雨落成诗（WeatherBottleUI）活动 =====
// 活动根 2026070300，子组 2026070301~305（payload 玩法说明已抓包确认）。
// 实现原则（2026-08-25 拍板）：能做的真接，做不了的占位。
//   - 能做的：顶部 7 瓶(5001~5007)状态读背包实时；5002/5007/5008 开箱/召唤走 ItemService.Use；
//     5003 闪电变异的"可变异地块筛选"函数（字段已 100% 定死）。
//   - 框架可抄鹊桥、开服填空：5001/5004/5005/5006 好友向 Use（item id 已知，Use 编码/目标地块开服确认）。
//   - 占位（待开服抓包）：雷电徽章 id、气象研究档位 RPC、claim cmd、5009/5010 可售性。

const (
	yuluItemCollect  = 5001 // 天气采集瓶（好友雷雨农场用）
	yuluItemSummon   = 5002 // 雷雨召唤瓶（自己农场召唤雷雨）
	yuluItemMutate   = 5003 // 闪电变异瓶（自己作物变异）
	yuluItemThunder  = 5004 // 霹雳引雷瓶（好友作物）
	yuluItemFrog     = 5005 // 青蛙使坏瓶（好友农场）
	yuluItemCloud    = 5006 // 乌云使坏瓶（好友农场）
	yuluItemSurprise = 5007 // 百宝惊喜瓶（开箱）
	yuluItemGiftBox  = 5008 // 雷纹礼盒（开箱）
	yuluItemWood     = 5009 // 雷击木（产物）
	yuluItemGoldWood = 5010 // 黄金雷击木（产物）
)

var yuluAllItemIDs = []int64{
	yuluItemCollect, yuluItemSummon, yuluItemMutate, yuluItemThunder,
	yuluItemFrog, yuluItemCloud, yuluItemSurprise, yuluItemGiftBox,
	yuluItemWood, yuluItemGoldWood,
}

// yuluBagLookup 在背包回复里查某物品的持有数与实例 uid。
func yuluBagLookup(br *proto.BagReply, id int64) (count, uid int64) {
	if br == nil {
		return 0, 0
	}
	for _, it := range br.Items {
		if it.ID == id {
			return it.Count, it.UID
		}
	}
	return 0, 0
}

// yuluMutateTargets 挑出自家"可变异"地块：排除 种子(PhaseSeed) / 枯萎(PhaseDead) / 天工(非空 MutantConfigIDs)。
// 字段依据 proto/plantpb.go：PhaseSeed=1, PhaseDead=7, MutantConfigIDs 字段20（已用 getMutantEffectsByIDs 取变异态）。
// TODO 开服确认：天工 是否 100% = 非空 MutantConfigIDs。
func yuluMutateTargets(lands []*proto.LandInfo, now int64) []int64 {
	var out []int64
	for _, l := range lands {
		p := l.Plant
		if p == nil {
			continue
		}
		ph := currentPhase(p.Phases, now)
		if ph == nil {
			continue
		}
		if ph.Phase == proto.PhaseSeed {
			continue
		}
		if ph.Phase == proto.PhaseDead {
			continue
		}
		if len(p.MutantConfigIDs) > 0 {
			continue // TODO 开服确认：天工 = 已变异 → 不可再变异
		}
		out = append(out, l.ID)
	}
	return out
}

// ===== 状态：GET /api/activity/yulu =====
// 返回顶部 8 统计所需数据：雷电徽章(占位) + 5001~5010 各物品实时数量/图片/名称（读背包）。
// 气象研究档位占位（待开服抓包 claim cmd/节点号）。
func handleYuluStatus(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", err.Error())
		return
	}
	rep, err := c.Request(ctx, "gamepb.itempb.ItemService", "Bag", proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", "GRPC:"+err.Error())
		return
	}
	br := proto.DecodeBagReply(rep.Body)
	items := map[string]interface{}{}
	for _, id := range yuluAllItemIDs {
		cnt, _ := yuluBagLookup(br, id)
		items[fmt.Sprintf("%d", id)] = map[string]interface{}{
			"id":    id,
			"count": cnt,
			"name":  itemDisplayName(id),
			"image": GetItemImageURL(int(id)),
		}
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"data": map[string]interface{}{
			// 雷电徽章：活动代币，id 待开服抓包（可能是背包物品或活动内部计数）
			"badge":       nil,
			"badgeNote":   "雷电徽章 id 待开服抓包（活动 8/26 10:00 开服后确认）",
			"items":       items,
			"research": map[string]interface{}{
				"tiers":       []interface{}{},
				"claimedAll":  false,
				"note":       "气象研究档位待开服抓包（task-progress RPC + claim cmd 未定）",
			},
		},
	})
}

// ===== 开箱/召唤：POST /api/activity/yulu/open =====
// 5007 百宝惊喜瓶 / 5008 雷纹礼盒（均 type=11 开箱物，can_use=1）走 ItemService.Use。
// 编码对齐通用 /api/bag/use：标准 EncodeUseRequest，遇 1000020 自动回退 EncodeUseRequestFallback。
func handleYuluOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
		ItemID    int64  `json:"itemId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONMap(w, "ok", false, "error", "bad json")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if req.AccountID != "" {
		accountID = req.AccountID
	}
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	if req.ItemID <= 0 {
		writeJSONMap(w, "ok", false, "error", "缺少 itemId")
		return
	}
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	_, err = c.Request(ctx, "gamepb.itempb.ItemService", "Use",
		proto.EncodeUseRequest(req.ItemID, 1), 12*time.Second)
	if err != nil && proto.IsBadParamError(err.Error()) {
		_, err = c.Request(ctx, "gamepb.itempb.ItemService", "Use",
			proto.EncodeUseRequestFallback(req.ItemID, 1), 12*time.Second)
	}
	if err != nil {
		writeJSONMap(w, "ok", false, "error", "使用失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "itemId": req.ItemID, "opened": true})
}

// ===== 闪电变异：POST /api/activity/yulu/mutate =====
// 5003 闪电变异瓶：挑自家可变异地块（排除种子/枯萎/天工），逐地块 Use{ item{5003,1,uid}, target{0, land_id} }。
// 筛选逻辑现在定死；Use 编码待开服一锤（当前照搬鹊桥喷洒的 item+target 结构）。
func handleYuluMutate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONMap(w, "ok", false, "error", "bad json")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if req.AccountID != "" {
		accountID = req.AccountID
	}
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	// 取 5003 的实例 uid
	var uid int64
	if brep, e := c.Request(ctx, "gamepb.itempb.ItemService", "Bag", proto.EncodeBagRequest(), 12*time.Second); e == nil {
		_, uid = yuluBagLookup(proto.DecodeBagReply(brep.Body), yuluItemMutate)
	}
	if uid == 0 {
		writeJSONMap(w, "ok", false, "error", "背包中无闪电变异瓶(5003)或缺少实例")
		return
	}
	// 拉自家地块
	rep, err := c.Request(ctx, plantService, "AllLands", proto.EncodeAllLandsRequest(0), 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", "GRPC:"+err.Error())
		return
	}
	targets := yuluMutateTargets(proto.DecodeAllLandsReply(rep.Body).Lands, time.Now().Unix())
	if len(targets) == 0 {
		writeJSON(w, map[string]interface{}{"ok": true, "account": accountID,
			"data": map[string]interface{}{"mutated": []int64{}, "mutateCount": 0, "msg": "无可变异地块（已排除种子/枯萎/天工）"}})
		return
	}
	var mutated []int64
	var errs []string
	item := proto.NewBuilder()
	item.FieldInt64Always(1, yuluItemMutate)
	item.FieldInt64Always(2, 1)
	item.FieldInt64(6, uid)
	for _, landID := range targets {
		sub := proto.NewBuilder()
		sub.FieldInt64Always(1, 0) // 自家 host_gid=0，无需 Enter
		sub.FieldBytes(2, appendVarintBytes(landID))
		ub := proto.NewBuilder()
		ub.FieldMessage(1, item.Bytes())
		ub.FieldMessage(2, sub.Bytes())
		if _, e2 := c.Request(ctx, "gamepb.itempb.ItemService", "Use", ub.Bytes(), 12*time.Second); e2 != nil {
			errs = append(errs, fmt.Sprintf("land%d:%v", landID, e2))
		} else {
			mutated = append(mutated, landID)
		}
		time.Sleep(300 * time.Millisecond)
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"data": map[string]interface{}{"mutated": mutated, "mutateCount": len(mutated), "errors": errs},
	})
}

// ===== 使用（好友向 / 自家召唤）：POST /api/activity/yulu/use =====
// 5002 雷雨召唤瓶：自家，plain Use（无 land，host_gid=0）。
// 5001/5004/5005/5006：好友向，Enter 好友 + AllLands + 逐地块 Use{ item{id,1,uid}, target{host_gid, land_id} }。
// 框架照搬鹊桥灵露喷洒；item id 已知，Use 编码/目标地块/是否校验雷雨态 待开服一锤。
func handleYuluUse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string  `json:"accountId"`
		ItemID    int64   `json:"itemId"`
		HostGID   int64   `json:"hostGid"`
		LandIDs   []int64 `json:"landIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONMap(w, "ok", false, "error", "bad json")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if req.AccountID != "" {
		accountID = req.AccountID
	}
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	if req.ItemID <= 0 {
		writeJSONMap(w, "ok", false, "error", "缺少 itemId")
		return
	}
	// 仅接受已确认的天气瓶 id
	switch req.ItemID {
	case yuluItemSummon, yuluItemCollect, yuluItemThunder, yuluItemFrog, yuluItemCloud:
	default:
		writeJSONMap(w, "ok", false, "error", fmt.Sprintf("物品 %d 不支持该接口", req.ItemID))
		return
	}
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	isSelf := req.ItemID == yuluItemSummon
	if isSelf {
		// 自家召唤：plain Use（无 land），对齐 /api/bag/use
		_, e := c.Request(ctx, "gamepb.itempb.ItemService", "Use",
			proto.EncodeUseRequest(req.ItemID, 1), 12*time.Second)
		if e != nil && proto.IsBadParamError(e.Error()) {
			_, e = c.Request(ctx, "gamepb.itempb.ItemService", "Use",
				proto.EncodeUseRequestFallback(req.ItemID, 1), 12*time.Second)
		}
		if e != nil {
			writeJSONMap(w, "ok", false, "error", "使用失败: "+e.Error())
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "itemId": req.ItemID, "used": true})
		return
	}
	// 好友向：必须指定 hostGid
	if req.HostGID <= 0 {
		writeJSONMap(w, "ok", false, "error", "好友向瓶子需指定 hostGid")
		return
	}
	if _, _, e := enterFriendFarm(c, req.HostGID, 2, ""); e != nil {
		writeJSONMap(w, "ok", false, "error", "Enter:"+e.Error())
		return
	}
	defer leaveFriendFarm(c, req.HostGID)
	rep, err := c.Request(ctx, plantService, "AllLands", proto.EncodeAllLandsRequest(req.HostGID), 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", "GRPC:"+err.Error())
		return
	}
	lands := proto.DecodeAllLandsReply(rep.Body).Lands
	want := map[int64]bool{}
	for _, id := range req.LandIDs {
		want[id] = true
	}
	var selected []int64
	for _, l := range lands {
		hasCrop := l.Plant != nil && len(l.Plant.Phases) > 0
		if !hasCrop {
			continue
		}
		if len(want) > 0 && !want[l.ID] {
			continue
		}
		selected = append(selected, l.ID)
	}
	if len(selected) == 0 {
		writeJSON(w, map[string]interface{}{"ok": true, "account": accountID,
			"data": map[string]interface{}{"used": []int64{}, "useCount": 0, "msg": "好友无可作用地块（无作物或未指定）"}})
		return
	}
	// 取 item uid
	var uid int64
	if brep, e := c.Request(ctx, "gamepb.itempb.ItemService", "Bag", proto.EncodeBagRequest(), 12*time.Second); e == nil {
		_, uid = yuluBagLookup(proto.DecodeBagReply(brep.Body), req.ItemID)
	}
	var used []int64
	var errs []string
	item := proto.NewBuilder()
	item.FieldInt64Always(1, req.ItemID)
	item.FieldInt64Always(2, 1)
	if uid > 0 {
		item.FieldInt64(6, uid)
	}
	for _, landID := range selected {
		sub := proto.NewBuilder()
		sub.FieldInt64Always(1, req.HostGID)
		sub.FieldBytes(2, appendVarintBytes(landID))
		ub := proto.NewBuilder()
		ub.FieldMessage(1, item.Bytes())
		ub.FieldMessage(2, sub.Bytes())
		if _, e2 := c.Request(ctx, "gamepb.itempb.ItemService", "Use", ub.Bytes(), 12*time.Second); e2 != nil {
			errs = append(errs, fmt.Sprintf("land%d:%v", landID, e2))
		} else {
			used = append(used, landID)
		}
		time.Sleep(300 * time.Millisecond)
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"data": map[string]interface{}{"used": used, "useCount": len(used), "errors": errs},
	})
}

// ===== 气象研究领奖：POST /api/activity/yulu/research =====
// 占位：雷电徽章 + 气象研究档位（claim cmd/节点号）待开服抓包。
func handleYuluResearch(w http.ResponseWriter, r *http.Request) {
	writeJSONMap(w, "ok", false, "error", "气象研究领奖待开服抓包（claim cmd / 节点号未定，活动 8/26 10:00 开服后回填）", "pending", true)
}
