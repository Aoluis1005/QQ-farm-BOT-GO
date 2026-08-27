package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ===== 雨落成诗（WeatherBottleUI）活动 =====
// 活动根 2026070300，子组 2026070301~305（payload 玩法说明已抓包确认）。
// 实现原则（2026-08-25 拍板）：能做的真接，做不了的占位。
//   - 已实装：顶部 7 瓶(5001~5007)状态读背包实时；5002/5007/5008 开箱/召唤走 ItemService.Use；
//     5003 闪电变异的"可变异地块筛选"函数（字段已 100% 定死）；
//     5001 采集（Operate 2026070303 cmd=9）、5004 引雷/5005 青蛙/5006 乌云（好友向 Use，目标编码已定）；
//     气象研究档位（GetGroup 根节点解析真实进度）、换瓶商店（Operate 2026070301 cmd=1）。
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

// yuluItemNameOf 活动物品中文名。活动物品不在游戏 itemInfoMap 内（itemDisplayName 返回空），
// 故硬编码名称兜底（来源=用户提供的 ItemInfo 全量清单）。优先用硬编码名，其次回退通用 itemDisplayName。
var yuluItemName = map[int64]string{
	yuluItemCollect:  "天气采集瓶",
	yuluItemSummon:   "雷雨召唤瓶",
	yuluItemMutate:   "闪电变异瓶",
	yuluItemThunder:  "霹雳引雷瓶",
	yuluItemFrog:     "青蛙使坏瓶",
	yuluItemCloud:    "乌云使坏瓶",
	yuluItemSurprise: "百宝惊喜瓶",
	yuluItemGiftBox:  "雷纹礼盒",
	yuluItemWood:     "雷击木",
	yuluItemGoldWood: "黄金雷击木",
}

func yuluItemNameOf(id int64) string {
	if nm, ok := yuluItemName[id]; ok {
		return nm
	}
	return itemDisplayName(id)
}

// ===== 气象研究（tech_tree 分叉研究树，领取接口已抓包实锤 2026-08-26） =====
// 领取：ActivityService.Operate({ id:2026070304, cmd:40,
//        tech_tree_submit_node{ node_id: 档位号 } })  -- proto: field140{sub field1=node_id}
// 服务端失败语义：node_id 起点 → "雷电徽章不足"；有前置未领取 → "节点未解锁"。
const (
	yuluResearchNodeID   = 2026070304 // 气象研究子节点 id（雨落成诗 0300 根下）
	yuluResearchCmd      = 40         // cmd=40 提交研究节点（领取该档位奖励）
	yuluResearchExtField = 140        // tech_tree_submit_node
	yuluBadgeID          = 1027       // 雷电徽章（研究/换瓶消耗货币，已实锤）
)

// ===== 兑换收集瓶子（0301 商店 field102，实测实锤 2026-08-26） =====
// Operate(id=2026070301, cmd=1, exchange_shop_operate{ slot=200, count })；
// 消耗金豆(1005)×200 → 天气采集瓶(5001)×1，每自然日限购 1 次（超限报"活动商品限购超限"）。
const (
	yuluExchNode      = 2026070301 // 换瓶商店子节点 id
	yuluExchCmd       = 1          // 兑换命令（与星纱商店同 cmd=1）
	yuluExchExtField  = 101        // exchange_shop_operate
	yuluExchSlot      = 200        // 天气采集瓶兑换档 slot id（field102 内商品标识）
	yuluExchCostItem  = 1005       // 金豆（消耗货币）
	yuluExchCostCount = 200        // 消耗 200 金豆
	yuluExchGetItem   = 5001       // 天气采集瓶（获得）
	yuluExchGetCount  = 1          // 每自然日 1 个
)

// ===== 采集瓶（5001）：进好友农场后 ActivityService.Operate（雷雨好友才可收集，服务端校验） =====
// Operate(id=2026070303, cmd=9, weather_task_operate_params{ target_gid=好友gid })；
// 回包 ActivityOperateReply field108=weather_task_result{ field2=gained, field3=consumed }。
const (
	yuluTaskNodeID   = 2026070303 // 天气采集任务子节点 id
	yuluTaskCmd      = 9          // 采集命令（operate_type=9）
	yuluTaskExtField = 107        // weather_task_operate_params
	yuluGroupRootID  = 2026070300 // 活动根节点 id（GetGroup 用根查子活动状态）
)

// yuluResearchTier 一个气象研究档位（研究树节点）。
// 分叉依赖见 Prevs：该档位需把全部前置档位领取后才可领取（服务端返回"节点未解锁"兜底）。
type yuluResearchTier struct {
	NodeID   int64   `json:"nodeId"`  // 研究节点 id（提交给 cmd=40 的 node_id）
	RewardID int64   `json:"rewardId"` // 奖励物品 id
	Reward   string  `json:"reward"`   // 奖励展示名
	Count    int64   `json:"count"`    // 奖励数量
	Cost     int64   `json:"cost"`     // 消耗雷电徽章(1027)数量
	Prevs    []int64 `json:"prevs"`    // 前置档位 node_id（分叉树依赖）
}

// yuluResearchTree 气象研究 9 档（开服抓包 0304·field118 实锤：档位号=node_id、消耗1027、奖励）。
// 树形：1000(起点·天气采集瓶)→1001(化肥礼包)→[1002青蛙,1003乌云]→[1004雷雨,1005有机化肥]
//       →[1006闪电感应,1007闪电感应]→1008(终点头像框)。
var yuluResearchTree = []yuluResearchTier{
	{NodeID: 1000, RewardID: yuluItemCollect, Reward: "天气采集瓶", Count: 1, Cost: 20, Prevs: nil},
	{NodeID: 1001, RewardID: 100003, Reward: "化肥礼包", Count: 5, Cost: 40, Prevs: []int64{1000}},
	{NodeID: 1002, RewardID: yuluItemFrog, Reward: "青蛙使坏瓶", Count: 20, Cost: 40, Prevs: []int64{1001}},
	{NodeID: 1003, RewardID: yuluItemCloud, Reward: "乌云使坏瓶", Count: 20, Cost: 40, Prevs: []int64{1001}},
	{NodeID: 1004, RewardID: yuluItemSummon, Reward: "雷雨召唤瓶", Count: 1, Cost: 60, Prevs: []int64{1002}},
	{NodeID: 1005, RewardID: 80013, Reward: "有机化肥(8小时)", Count: 3, Cost: 60, Prevs: []int64{1003}},
	{NodeID: 1006, RewardID: 4002, Reward: "闪电感应", Count: 1, Cost: 80, Prevs: []int64{1004}},
	{NodeID: 1007, RewardID: 4003, Reward: "闪电感应", Count: 1, Cost: 80, Prevs: []int64{1005}},
	{NodeID: 1008, RewardID: 2159, Reward: "头像框", Count: 1, Cost: 100, Prevs: []int64{1006, 1007}},
}

// yuluResearchTierByID 按 node_id 查档位。
func yuluResearchTierByID(id int64) *yuluResearchTier {
	for i := range yuluResearchTree {
		if yuluResearchTree[i].NodeID == id {
			return &yuluResearchTree[i]
		}
	}
	return nil
}

// handleYuluResearchClaim 领取气象研究档位：
// Operate(id=2026070304, cmd=40, field140{ node_id })，错误映射为可读提示。
func handleYuluResearchClaim(ctx context.Context, accountID string, nodeID int64) (rewards []map[string]interface{}, body []byte, errMsg string) {
	sub := proto.NewBuilder()
	sub.FieldInt64(1, nodeID) // TechTreeSubmitNodeReq.node_id
	b := proto.NewBuilder()
	b.FieldInt64(1, yuluResearchNodeID)
	b.FieldInt64(2, yuluResearchCmd)
	b.FieldMessage(yuluResearchExtField, sub.Bytes())
	body, err := rpcRequest(ctx, accountID, actSvc, "Operate", b.Bytes(), 20*time.Second)
	if err != nil {
		e := err.Error()
		switch {
		case strings.Contains(e, "雷电徽章不足"):
			return nil, nil, "雷电徽章不足"
		case strings.Contains(e, "节点未解锁"):
			return nil, nil, "节点未解锁（需先领取前置档位）"
		default:
			return nil, nil, e
		}
	}
	return parseActRewardField(body, 126), body, ""
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
			"name":  yuluItemNameOf(id),
			"image": GetItemImageURL(int(id)),
		}
	}
	badgeCnt, _ := yuluBagLookup(br, yuluBadgeID)
	wid, wact := yuluGetWeather(ctx, c)
	weatherName := "无"
	if wid != 0 {
		weatherName = "雷雨"
	}
	researchState := yuluResearchState(ctx, accountID)
	tiers := make([]map[string]interface{}, 0, len(yuluResearchTree))
	for _, t := range yuluResearchTree {
		st := researchState[t.NodeID]
		claimed, status := false, int64(0)
		if st != nil {
			claimed, _ = st["claimed"].(bool)
			status, _ = st["status"].(int64)
		}
		tiers = append(tiers, map[string]interface{}{
			"nodeId":   t.NodeID,
			"name":     t.Reward,
			"reward":   t.Reward,
			"rewardId": t.RewardID,
			"count":    t.Count,
			"cost":     t.Cost,
			"prevs":    t.Prevs,
			"claimed":  claimed,
			"status":   status,
		})
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"data": map[string]interface{}{
			"badge":      badgeCnt,
			"badgeNote":  "雷电徽章：气象研究/换天气瓶消耗",
			"badgeImage": GetItemImageURL(yuluBadgeID),
			"weather": map[string]interface{}{
				"id": wid, "name": weatherName, "active": wact,
			},
			"items": items,
			"research": map[string]interface{}{
				"tiers":      tiers,
				"claimedAll": false,
				"note":       "",
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
// 5005 青蛙使坏瓶：农场级事件，target 只带 host_gid（无 land_ids），整农场触发一次。
// 5001/5004/5006：好友向，Enter 好友 + AllLands + Use{ item{id,count,uid}, target{host_gid, land_ids} }；
//   5001 采集走 Operate（雷雨好友才可收集，服务端校验）；5006 乌云目标限生长中地块。
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
		// 自家召唤：当前已有特殊天气（雷雨）时不可召唤
		if wid, _ := yuluGetWeather(ctx, c); wid != 0 {
			writeJSONMap(w, "ok", false, "error", "当前已有特殊天气，无法召唤雷雨")
			return
		}
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
	// 采集瓶(5001)：进好友农场后对目标好友发 Operate 收集（雷雨好友才可收集，服务端校验）。
	// 请求 {activity_id=2026070303, operate_type=9, params{target_gid}}。
	if req.ItemID == yuluItemCollect {
		sub := proto.NewBuilder()
		sub.FieldInt64(3, req.HostGID) // weather_task_operate_params.target_gid
		b := proto.NewBuilder()
		b.FieldInt64(1, yuluTaskNodeID)
		b.FieldInt64(2, yuluTaskCmd)
		b.FieldMessage(yuluTaskExtField, sub.Bytes())
		body, e2 := rpcRequest(ctx, accountID, actSvc, "Operate", b.Bytes(), 15*time.Second)
		if e2 != nil {
			writeJSONMap(w, "ok", false, "error", actErrMsg(e2))
			return
		}
		gained, consumed := decodeYuluTaskResult(body)
		writeJSON(w, map[string]interface{}{
			"ok": true, "account": accountID, "itemId": req.ItemID,
			"data": map[string]interface{}{
				"used": []int64{req.HostGID}, "useCount": 1,
				"gained": gained, "consumed": consumed,
			},
		})
		return
	}
	// 青蛙使坏瓶(5005)：农场级事件，target 只带 host_gid（不带 land_ids），整农场触发一次。
	if req.ItemID == yuluItemFrog {
		var uid int64
		if brep, e := c.Request(ctx, "gamepb.itempb.ItemService", "Bag", proto.EncodeBagRequest(), 12*time.Second); e == nil {
			for _, it := range proto.DecodeBagReply(brep.Body).Items {
				if it.ID == req.ItemID && it.UID > 0 {
					uid = it.UID
					break
				}
			}
		}
		item := proto.NewBuilder()
		item.FieldInt64Always(1, req.ItemID)
		item.FieldInt64Always(2, 1)
		if uid > 0 {
			item.FieldInt64(6, uid)
		}
		target := proto.NewBuilder()
		target.FieldInt64Always(1, req.HostGID)
		ub := proto.NewBuilder()
		ub.FieldMessage(1, item.Bytes())
		ub.FieldMessage(2, target.Bytes())
		if _, e2 := c.Request(ctx, "gamepb.itempb.ItemService", "Use", ub.Bytes(), 12*time.Second); e2 != nil {
			writeJSONMap(w, "ok", false, "error", "使用失败: "+e2.Error())
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "itemId": req.ItemID,
			"data": map[string]interface{}{"used": []int64{req.HostGID}, "useCount": 1}})
		return
	}
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
	if req.ItemID == yuluItemCloud {
		// 乌云使坏瓶(5006)：目标地块必须是生长中的作物（phase 在种子与成熟之间），每好友 1 块
		for _, l := range lands {
			if len(want) > 0 && !want[l.ID] {
				continue
			}
			if l.Plant == nil || len(l.Plant.Phases) == 0 {
				continue
			}
			ph := currentPhase(l.Plant.Phases, time.Now().Unix())
			if ph == nil || ph.Phase <= proto.PhaseSeed || ph.Phase >= proto.PhaseMature {
				continue
			}
			selected = append(selected, l.ID)
			if len(selected) >= 1 {
				break
			}
		}
	} else {
		// 引雷瓶(5004)：有作物的地块即可，逐地块使用
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
	}
	if len(selected) == 0 {
		writeJSON(w, map[string]interface{}{"ok": true, "account": accountID,
			"data": map[string]interface{}{"used": []int64{}, "useCount": 0, "msg": "好友无可作用地块（无作物或未指定）"}})
		return
	}
	// 取 item uid（参考鹊桥灵露喷洒）
	var uid int64
	if brep, e := c.Request(ctx, "gamepb.itempb.ItemService", "Bag", proto.EncodeBagRequest(), 12*time.Second); e == nil {
		for _, it := range proto.DecodeBagReply(brep.Body).Items {
			if it.ID == req.ItemID && it.UID > 0 {
				uid = it.UID
				break
			}
		}
	}
	var used []int64
	var errs []string
	// 参考鹊桥灵露喷洒：逐地块 Use，嵌套 {field1=item{id,count,uid}, field2={host_gid, land_id}}
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
// ===== 气象研究领取：POST /api/activity/yulu/research =====
// body: { accountId, nodeId }。
// 内部 Operate(id=2026070304, cmd=40, tech_tree_submit_node{node_id})，
// 失败提示：雷电徽章不足 / 节点未解锁（前置未领取）。
func handleYuluResearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
		NodeID    int64  `json:"nodeId"`
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
	tier := yuluResearchTierByID(req.NodeID)
	if tier == nil {
		writeJSONMap(w, "ok", false, "error", "无效的研究节点 nodeId")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rewards, body, errMsg := handleYuluResearchClaim(ctx, accountID, req.NodeID)
	if errMsg != "" {
		writeJSONMap(w, "ok", false, "error", errMsg, "nodeId", req.NodeID)
		return
	}
	if rewards == nil {
		rewards = []map[string]interface{}{}
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"data": map[string]interface{}{
			"nodeId":         req.NodeID,
			"reward":         tier.Reward,
			"count":          tier.Count,
			"rewards":        rewards,
			"unlockedNodeIds": yuluResearchUnlocked(body),
		},
	})
}

// ===== 兑换收集瓶子：POST /api/activity/yulu/exchange =====
// 消耗金豆(1005)×200 → 天气采集瓶(5001)×1，每自然日限购 1 次。
func handleYuluExchange(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	sub := proto.NewBuilder()
	sub.FieldInt64(1, yuluExchSlot)
	sub.FieldInt64(2, yuluExchGetCount)
	b := proto.NewBuilder()
	b.FieldInt64(1, yuluExchNode)
	b.FieldInt64(2, yuluExchCmd)
	b.FieldMessage(yuluExchExtField, sub.Bytes())
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	_, err := rpcRequest(ctx, accountID, actSvc, "Operate", b.Bytes(), 15*time.Second)
	if err != nil {
		e := err.Error()
		switch {
		case strings.Contains(e, "限购"):
			writeJSONMap(w, "ok", false, "error", "今日已兑换过（每自然日限兑 1 个）")
		case strings.Contains(e, "不足"):
			writeJSONMap(w, "ok", false, "error", "金豆不足（需 200 金豆）")
		default:
			writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		}
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"data": map[string]interface{}{
			"costItem": yuluExchCostItem, "costCount": yuluExchCostCount,
			"getItem": yuluExchGetItem, "getCount": yuluExchGetCount,
		},
	})
}

// decodeYuluTaskResult 解析采集回包：ActivityOperateReply field108=WeatherTaskOperateResult
// { field2=gained(Item{id,count}), field3=consumed(Item) }，无法解析时返回 nil。
func decodeYuluTaskResult(body []byte) (gained, consumed map[string]interface{}) {
	var res []byte
	proto.NewReader(body).EachField(func(field, wire int, r *proto.Reader) bool {
		if field == 108 && wire == proto.WireLen {
			res = r.ReadBytes()
		} else {
			r.Skip(wire)
		}
		return true
	})
	if res == nil {
		return nil, nil
	}
	parseItem := func(buf []byte) map[string]interface{} {
		var id, cnt int64
		proto.NewReader(buf).EachField(func(field, wire int, r *proto.Reader) bool {
			switch field {
			case 1:
				id = r.ReadInt64()
			case 2:
				cnt = r.ReadInt64()
			default:
				r.Skip(wire)
			}
			return true
		})
		if id <= 0 {
			return nil
		}
		return map[string]interface{}{"id": id, "count": cnt}
	}
	proto.NewReader(res).EachField(func(field, wire int, r *proto.Reader) bool {
		if wire != proto.WireLen {
			r.Skip(wire)
			return true
		}
		sub := r.ReadBytes()
		switch field {
		case 2:
			gained = parseItem(sub)
		case 3:
			consumed = parseItem(sub)
		}
		return true
	})
	return
}

// yuluGetWeather 查询当前农场天气状态（weatherpb.WeatherService.GetWeatherStatus）。
// 回包 field1=WeatherStatus{ field1=weather_id(0无/1雷雨), field5=active }；失败返回 (0,false)。
func yuluGetWeather(ctx context.Context, c *gw.Client) (id int64, active bool) {
	rep, err := c.Request(ctx, "gamepb.weatherpb.WeatherService", "GetWeatherStatus", []byte{}, 12*time.Second)
	if err != nil {
		return 0, false
	}
	proto.NewReader(rep.Body).EachField(func(field, wire int, r *proto.Reader) bool {
		if field != 1 || wire != proto.WireLen {
			r.Skip(wire)
			return true
		}
		proto.NewReader(r.ReadBytes()).EachField(func(f2, w2 int, r2 *proto.Reader) bool {
			switch f2 {
			case 1:
				id = r2.ReadInt64()
			case 5:
				active = r2.ReadInt64() != 0
			default:
				r2.Skip(w2)
			}
			return true
		})
		return true
	})
	return
}

// yuluResearchState 从 GetGroup(活动根 2026070300) 解析气象研究真实进度：
// 找到研究子节点(2026070304)的 field118=weather_research{state(field1){nodes(field2 重复)}}，
// 每节点 {1=node_id, 3=status(2 可领取), 4=claimed}。
// 返回 nodeId -> {status, claimed}；解析失败返回 nil。
func yuluResearchState(ctx context.Context, accountID string) map[int64]map[string]interface{} {
	b := proto.NewBuilder()
	b.FieldInt64(1, yuluGroupRootID)
	b.FieldString(2, "")
	key := actGroupCacheKey(accountID, yuluGroupRootID)
	body, ok := actCacheGet(key, 30*time.Second)
	if !ok {
		var err error
		body, err = rpcRequest(ctx, accountID, actSvc, "GetGroup", b.Bytes(), 20*time.Second)
		if err != nil {
			return nil
		}
		actCacheSet(key, body, 30*time.Second)
	}
	reply := readActFields(body)
	groupRaw := actBytes(reply, 1)
	if len(groupRaw) == 0 {
		return nil
	}
	out := map[int64]map[string]interface{}{}
	var walk func(raw []byte) bool
	walk = func(raw []byte) bool {
		fs := readActFields(raw)
		if infoRaw := actBytes(fs, 1); len(infoRaw) > 0 {
			if actNum(readActFields(infoRaw), 1) == yuluResearchNodeID {
				if wr := actBytes(fs, 118); len(wr) > 0 {
					if stateRaw := actBytes(readActFields(wr), 1); len(stateRaw) > 0 {
						for _, nRaw := range actBytesAll(readActFields(stateRaw), 2) {
							nf := readActFields(nRaw)
							nid := actNum(nf, 1)
							if nid <= 0 {
								continue
							}
							out[nid] = map[string]interface{}{
								"status":  actNum(nf, 3),
								"claimed": actNum(nf, 4) != 0,
							}
						}
					}
				}
				return true
			}
		}
		for _, c := range actBytesAll(fs, 2) {
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(groupRaw)
	return out
}

// yuluResearchUnlocked 解析研究领取回包 field140=weather_research_result{field3=unlocked_node_ids(packed)}。
func yuluResearchUnlocked(body []byte) []int64 {
	fs := readActFields(body)
	resRaw := actBytes(fs, 140)
	if len(resRaw) == 0 {
		return nil
	}
	for _, f := range readActFields(resRaw) {
		if f.No == 3 && f.Wire == 2 {
			var out []int64
			for i := 0; i < len(f.Bytes); {
				var v uint64
				var shift uint
				for i < len(f.Bytes) {
					x := f.Bytes[i]
					i++
					v |= uint64(x&0x7f) << shift
					if x < 0x80 {
						break
					}
					shift += 7
				}
				out = append(out, int64(v))
			}
			return out
		}
	}
	return nil
}
