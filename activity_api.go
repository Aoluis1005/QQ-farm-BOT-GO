package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/proto"
)

// 活动中心 API：
//
//	GET /api/activity/list   ActivityService.List + 按时间窗过滤
//	GET /api/activity/group  ActivityService.GetGroup 递归树 + 商店商品
//	GET /api/activity/season SeasonService.GetSeasonInfo（千星游记）
//	GET /api/activity/solar  SolarTermsService.GetSolarTerms（节令小礼）
//
// 对齐 Node core/src/services/activity.js。禁止并发（游戏内容相关，均顺序单发）。

const (
	actSvc   = "gamepb.activitypb.ActivityService"
	seasonSvc = "gamepb.seasonpb.SeasonService"
	solarSvc  = "gamepb.solartermspb.SolarTermsService"
	shareSvc  = "gamepb.sharepb.ShareService"
)

func registerActivityAPI(api *http.ServeMux) {
	api.HandleFunc("/api/activity/list", handleActivityList)
	api.HandleFunc("/api/activity/group", handleActivityGroup)
	api.HandleFunc("/api/activity/season", handleActivitySeason)
	api.HandleFunc("/api/activity/season/claim", handleActivitySeasonClaim)
	api.HandleFunc("/api/activity/solar", handleActivitySolar)
	api.HandleFunc("/api/activity/solar/claim", handleActivitySolarClaim)
	api.HandleFunc("/api/activity/guanxing", handleActivityGuanxing)
	api.HandleFunc("/api/activity/guanxing/claim", handleActivityGuanxingClaim)
	api.HandleFunc("/api/activity/shop", handleActivityShop)
	api.HandleFunc("/api/activity/shop/exchange", handleActivityShopExchange)
	api.HandleFunc("/api/activity/qingmei", handleQingmei)
	api.HandleFunc("/api/activity/qingmei/claim", handleQingmeiClaim)
	api.HandleFunc("/api/activity/qingmei/wine", handleQingmeiWine)
	// TODO: 临时鹊桥 cmd 探测接口，探测完成后删除
	api.HandleFunc("/api/debug/act_operate", handleDebugActOperate)
	api.HandleFunc("/api/debug/plant_rpc", handleDebugPlantRPC)
	api.HandleFunc("/api/debug/bag_dump", handleDebugBagDump)
	api.HandleFunc("/api/activity/qixi", handleQiXiStatus)
	api.HandleFunc("/api/activity/qixi/spray", handleQiXiSpray)   // 灵露喷洒（ItemService.Use + land_ids）
	api.HandleFunc("/api/activity/qixi/bridge", handleQiXiBridge) // 筑建鹊桥（Operate id=2026081801 cmd=25）
	api.HandleFunc("/api/activity/qixi/gift", handleQiXiGift)     // 赠送鹊羽香囊（Enter 好友 + Operate）
	api.HandleFunc("/api/debug/item_use", handleDebugItemUse)
}

// ----- List：活动列表 + 时间过滤 -----

func handleActivityList(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "ongoing"
	}
	refresh := r.URL.Query().Get("refresh") == "1"
	key := "actlist:" + accountID + ":" + scope

	// 正常访问（非强制刷新）→ 直接返回缓存，不再向游戏发 List（防风控）
	if !refresh {
		if cached, ok := actCacheGet(key, actListTTL); ok {
			var out []*outItem
			if err := json.Unmarshal(cached, &out); err == nil {
				writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "now": time.Now().Unix(), "scope": scope, "items": out, "cached": true})
				return
			}
			actCacheDel(key)
		}
	}
	// 强制刷新：60s 冷却内连点 → 仍返回缓存（防高频刺激 ActivityService）
	if refresh {
		actListFetchMu.Lock()
		last, ok := actListFetchAt[key]
		inCooldown := ok && time.Since(last) < actListRefreshCooldown
		if !inCooldown {
			actListFetchAt[key] = time.Now()
		}
		actListFetchMu.Unlock()
		if inCooldown {
			if cached, ok := actCacheGet(key, actListTTL); ok {
				var out []*outItem
				if err := json.Unmarshal(cached, &out); err == nil {
					writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "now": time.Now().Unix(), "scope": scope, "items": out, "cached": true, "cooldown": true})
					return
				}
			}
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, actSvc, "List", []byte{}, 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	items := ParseActivityList(body)
	now := time.Now().Unix()
	scope = r.URL.Query().Get("scope")
	if scope == "" {
		scope = "ongoing"
	}
	var out []*outItem
	// 组根（id%100==0）自身常为哨兵时间（-62135596800），真实时间在其子活动上。
	// 因此先收集：当前在期(on)的子活动，再据此判定组根是否 ongoing。
	onChild := map[int64]bool{}
	for _, it := range items {
		if it.ID%100 != 0 && it.StartTime > 0 && it.StartTime <= now && it.EndTime >= now {
			onChild[it.ID-it.ID%100] = true
		}
	}
	itemOngoing := func(it *ActivityInfo) bool {
		if it.StartTime > 0 && it.EndTime > 0 {
			return it.StartTime <= now && it.EndTime >= now
		}
		// 哨兵时间：仅在期当它是一个有活跃子活动的组根
		if it.ID%100 == 0 {
			return onChild[it.ID]
		}
		return false
	}
	for _, it := range items {
		ongoing := itemOngoing(it)
		upcoming := it.EndTime > 0 && it.StartTime > now
		finished := it.EndTime > 0 && it.EndTime < now
		show := false
	switch scope {
	case "all", "default":
		show = true
	case "ongoing":
		show = ongoing
	case "upcoming":
		show = upcoming
	case "finished":
		show = finished
	}
		if !show {
			continue
		}
		out = append(out, &outItem{
			ID: it.ID, Title: it.Title, StartTime: it.StartTime, EndTime: it.EndTime,
			Group: it.ID%100 == 0, Ongoing: ongoing, Upcoming: upcoming, Finished: finished,
		})
	}
	if b, err := json.Marshal(out); err == nil {
		actCacheSet(key, b, actListTTL)
	}
	writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "now": now, "scope": scope, "items": out})
}

// outItem 活动列表条目（包级：便于 list 缓存反序列化）
type outItem struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Group     bool   `json:"group"` // 主活动组(id%100==0)
	Ongoing   bool   `json:"ongoing"`
	Upcoming  bool   `json:"upcoming"`
	Finished  bool   `json:"finished"`
}

// 活动列表缓存：正常进出活动页直接命中缓存，不再向游戏发 List RPC（防风控）。
// “获取新活动”强制刷新，但带 60s 冷却：冷却内连点仍返回缓存，避免高频刺激 ActivityService。
const actListTTL = 10 * time.Minute
const actListRefreshCooldown = 60 * time.Second

var actListFetchMu sync.Mutex
var actListFetchAt = map[string]time.Time{} // actlist key -> 上次真实下发时间

// ----- Group：活动分组树 + 商店 -----

func handleActivityGroup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accountID := resolveAccountID(q.Get("accountId"))
	id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
	if id == 0 {
		writeJSONMap(w, "ok", false, "error", "id required")
		return
	}
	// GetGroup 支持 uid（可空；实测空串即可返回完整分组）。对齐 Node sendMsgAsync 默认 20s；结果短缓存避免每次重拉大树
	b := proto.NewBuilder()
	b.FieldInt64(1, id)
	b.FieldString(2, q.Get("uid"))
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	key := actGroupCacheKey(accountID, id)
	body, ok := actCacheGet(key, 30*time.Second)
	if !ok {
		var err error
		body, err = rpcRequest(ctx, accountID, actSvc, "GetGroup", b.Bytes(), 20*time.Second)
		if err != nil {
			writeJSONMap(w, "ok", false, "error", actErrMsg(err))
			return
		}
		actCacheSet(key, body, 30*time.Second)
	}
	node := ParseActivityGroup(body)
	writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "id": id, "tree": node})
}

func writeJSONMap(w http.ResponseWriter, kvs ...interface{}) {
	m := map[string]interface{}{}
	for i := 0; i+1 < len(kvs); i += 2 {
		m[fmt.Sprint(kvs[i])] = kvs[i+1]
	}
	writeJSON(w, m)
}

// rpcRequest 获取账号连接并调用 RPC，返回解密后的 body
func rpcRequest(ctx context.Context, accountID, service, method string, body []byte, timeout time.Duration) ([]byte, error) {
	c, err := clientPool.Get(accountID)
	if err != nil {
		return nil, err
	}
	msg, err := c.Request(ctx, service, method, body, timeout)
	if err != nil {
		return nil, err
	}
	return msg.Body, nil
}

func handleActivitySeason(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, seasonSvc, "GetSeasonInfo", []byte{}, 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "data": ParseSeason(body)})
}

// ----- Solar：节令小礼（节气） -----

func handleActivitySolar(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, solarSvc, "GetSolarTerms", []byte{}, 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "data": ParseSolar(body)})
}

// ----- 千星游记：领取全部可领档位（SeasonService.ClaimBattlePassRewards，空请求） -----

func handleActivitySeasonClaim(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	// before（用于算领取档位数差）
	beforeBody, err := rpcRequest(ctx, accountID, seasonSvc, "GetSeasonInfo", []byte{}, 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	before := ParseSeason(beforeBody)
	// 无可领档位时直接返回友好提示，不触发一次无意义的 Claim RPC
	if before != nil && before.Passport != nil && before.Passport.ClaimableLevels <= 0 {
		writeJSONMap(w, "ok", false, "error", "暂无奖励可领取")
		return
	}
	body, err := rpcRequest(ctx, accountID, seasonSvc, "ClaimBattlePassRewards", []byte{}, 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	res := ParseSeasonClaim(body)
	afterBody, err := rpcRequest(ctx, accountID, seasonSvc, "GetSeasonInfo", []byte{}, 15*time.Second)
	if err != nil {
		afterBody = nil
	}
	after := ParseSeason(afterBody)
	bl, al := int64(0), int64(0)
	if before != nil && before.Passport != nil {
		bl = before.Passport.FreeClaimedLevel
	}
	if after != nil && after.Passport != nil {
		al = after.Passport.FreeClaimedLevel
	}
	var passport *SeasonPassport
	if after != nil {
		passport = after.Passport
	}
	if passport == nil {
		passport = res.Passport
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"rewards":        res.Rewards,
		"passport":       passport,
		"claimed_levels": al - bl,
	})
}

// ----- 节令小礼：领取单个节气（SolarTermsService.ClaimSolarTerms，field1=termId） -----

func handleActivitySolarClaim(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	termID, _ := strconv.ParseInt(r.URL.Query().Get("termId"), 10, 64)
	b := proto.NewBuilder()
	b.FieldInt64(1, termID)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, solarSvc, "ClaimSolarTerms", b.Bytes(), 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	res := ParseSolarClaim(body)
	// 刷新最新节气状态
	solarBody, err := rpcRequest(ctx, accountID, solarSvc, "GetSolarTerms", []byte{}, 15*time.Second)
	if err != nil {
		solarBody = nil
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"rewards": res.Rewards, "term": res.Term, "solar": ParseSolar(solarBody),
	})
}

// ----- 观星礼录：二十八星宿数据（GetGroup + field110 星宿块） -----

func handleActivityGuanxing(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accountID := resolveAccountID(q.Get("accountId"))
	id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
	if id == 0 {
		id = guanxingActivityID
	}
	b := proto.NewBuilder()
	b.FieldInt64(1, id)
	b.FieldString(2, q.Get("uid"))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, actSvc, "GetGroup", b.Bytes(), 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "id": id, "data": ParseConstellation(body)})
}

// ----- 观星礼录：一键领取全部已解锁星宿（ActService.Operate cmd=21, field119空串） -----

func handleActivityGuanxingClaim(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accountID := resolveAccountID(q.Get("accountId"))
	// before
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	gb := proto.NewBuilder()
	gb.FieldInt64(1, guanxingActivityID)
	gb.FieldString(2, q.Get("uid"))
	beforeBody, err := rpcRequest(ctx, accountID, actSvc, "GetGroup", gb.Bytes(), 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	before := ParseConstellation(beforeBody)
	// operate
	ob := proto.NewBuilder()
	ob.FieldInt64(1, guanxingActivityID)
	ob.FieldInt64(2, guanxingClaimCmd)
	ob.FieldBytes(guanxingExtField, []byte{})
	_, err = rpcRequest(ctx, accountID, actSvc, "Operate", ob.Bytes(), 15*time.Second)
	if err != nil {
		es := actErrMsg(err)
		if !strings.Contains(es, itoa(guanxingNoReward)) && !strings.Contains(es, "无可领取") {
			writeJSONMap(w, "ok", false, "error", es)
			return
		}
	}
	// after
	afterBody, err := rpcRequest(ctx, accountID, actSvc, "GetGroup", gb.Bytes(), 15*time.Second)
	if err != nil {
		afterBody = nil
	}
	after := ParseConstellation(afterBody)
	var claimed []*Item
	if before != nil && after != nil {
		for _, bn := range before.Nodes {
			if !bn.Claimable {
				continue
			}
			claimed = append(claimed, bn.Rewards...)
		}
		claimed = mergeRewardItems(claimed)
	}
	var n *ConstellationInfo = after
	if n == nil {
		n = before
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"claimed_rewards": claimed, "data": n,
	})
}

// ----- 星砂商店：商品列表（含价格） + 星砂余额 -----

const (
	actExchangeActID = 2026072702 // 星纱商店活动（HELU_EXCHANGE_ACTIVITY_ID）
	actStarSandID    = 1023       // 星砂（活动通用货币）
	actExchangeCmd   = 1          // 兑换命令（HELU_EXCHANGE_CMD）
)

// starSandBalance 查询账号背包中星砂(1023)数量（对齐 Node getHeluBalance）
func starSandBalance(ctx context.Context, accountID string) int64 {
	c, err := clientPool.Get(accountID)
	if err != nil {
		return 0
	}
	rep, err := c.Request(ctx, "gamepb.itempb.ItemService", "Bag", proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		return 0
	}
	br := proto.DecodeBagReply(rep.Body)
	for _, it := range br.Items {
		if it.ID == actStarSandID && it.Count > 0 {
			return it.Count
		}
	}
	return 0
}

// actFindShopItems 遍历分组树找第一个含 exchange_shop 的节点
func actFindShopItems(node *ActivityNode) []*ShopItem {
	if node == nil {
		return nil
	}
	if len(node.ExchangeShop) > 0 {
		return node.ExchangeShop
	}
	for _, c := range node.Children {
		if it := actFindShopItems(c); it != nil {
			return it
		}
	}
	return nil
}

// actErrMsg 归一化 RPC 错误为简洁中文提示：
// "gamepb.activitypb.ActivityService.Operate code=1000019 星砂不足" -> "星砂不足"
func actErrMsg(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	// 优先保留协议返回的业务消息（通常为中文，最友好）
	if i := strings.Index(s, "code="); i >= 0 {
		if j := strings.IndexByte(s[i:], ' '); j >= 0 {
			if tail := strings.TrimSpace(s[i+j:]); tail != "" {
				return tail
			}
		}
	}
	// 其余底层英文错误 → 映射为中文友好提示；若已是中文业务消息则直接原样返回
	for _, r := range s {
		if r > 0x2E80 { // CJK 表意文字起始
			return s
		}
	}
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "connection refused"), strings.Contains(low, "connect") && strings.Contains(low, "refused"), strings.Contains(low, "no such host"):
		return "连接失败：账号可能已离线或网络异常"
	case strings.Contains(low, "deadline"), strings.Contains(low, "timeout"), strings.Contains(low, "timed out"), strings.Contains(low, "i/o timeout"):
		return "请求超时，请稍后重试"
	case strings.Contains(low, "permission"), strings.Contains(low, "forbidden"), strings.Contains(low, "unauthorized"):
		return "无权限执行此操作"
	case strings.Contains(low, "offline"), strings.Contains(low, "disconnected"), strings.Contains(low, "closed"):
		return "账号已离线，请先上线再操作"
	}
	return "活动数据获取失败，请稍后重试"
}

func handleActivityShop(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accountID := resolveAccountID(q.Get("accountId"))
	id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
	if id == 0 {
		id = actExchangeActID
	}
	b := proto.NewBuilder()
	b.FieldInt64(1, id)
	b.FieldString(2, q.Get("uid"))
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, actSvc, "GetGroup", b.Bytes(), 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	node := ParseActivityGroup(body)
	items := actFindShopItems(node)
	if items == nil {
		items = []*ShopItem{}
	}
	bal := starSandBalance(ctx, accountID)
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID, "id": id, "items": items,
		"balance": map[string]interface{}{"item_id": actStarSandID, "currency_name": itemDisplayName(actStarSandID), "count": bal},
	})
}

// handleActivityShopExchange 兑换星砂商店商品（Operate cmd=1, exchange_shop_operate{id,count}）
func handleActivityShopExchange(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accountID := resolveAccountID(q.Get("accountId"))
	id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
	if id == 0 {
		id = actExchangeActID
	}
	slotID, _ := strconv.ParseInt(q.Get("slotId"), 10, 64)
	if slotID <= 0 {
		writeJSONMap(w, "ok", false, "error", "slotId required")
		return
	}
	count := int64(1)
	if c, _ := strconv.ParseInt(q.Get("count"), 10, 64); c > 0 {
		count = c
	}
	sub := proto.NewBuilder()
	sub.FieldInt64(1, slotID)
	sub.FieldInt64(2, count)
	b := proto.NewBuilder()
	b.FieldInt64(1, id)
	b.FieldInt64(2, actExchangeCmd)
	b.FieldMessage(101, sub.Bytes())
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	_, err := rpcRequest(ctx, accountID, actSvc, "Operate", b.Bytes(), 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	// 刷新余额 + 商店
	bal := starSandBalance(ctx, accountID)
	var items []*ShopItem
	{
		rgb := proto.NewBuilder()
		rgb.FieldInt64(1, id)
		rgb.FieldString(2, q.Get("uid"))
		if gb, e := rpcRequest(ctx, accountID, actSvc, "GetGroup", rgb.Bytes(), 15*time.Second); e == nil {
			items = actFindShopItems(ParseActivityGroup(gb))
		}
	}
	if items == nil {
		items = []*ShopItem{}
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID, "slot_id": slotID, "count": count,
		"balance": map[string]interface{}{"item_id": actStarSandID, "currency_name": itemDisplayName(actStarSandID), "count": bal},
		"items":   items,
	})
}

// ===== 青梅酿万金（青酿换万金）：领种子 + 酿酒出售 =====
// 对齐 Node core/src/services/activity.js 青梅段常量与流程。
//  领种子：Operate cmd=4  qingmei_claim_params{type:2}
//  酿酒   ：Operate cmd=14(预览 qingmei_wine_start) / 15(精酿 qingmei_wine_brew{}) / 16(出售 qingmei_wine_sell{multiple})
const (
	qingmeiSeedItemID    = 21221 // 青梅种子
	qingmeiFruitItemID   = 41221 // 青梅（酿制材料）
	// 青梅活动固定 ID（对齐 Node: QINGMEI_ACTIVITY_ID/SEED_CLAIM/WINE）
	qingmeiRootActivityID  = 2026081200
	qingmeiClaimActivityID = 2026081201
	qingmeiWineActivityID  = 2026081202
	qingmeiSeedReward    = 24    // 每次领取种子数
	qingmeiClaimCmd      = 4
	qingmeiPreviewCmd    = 14
	qingmeiBrewCmd       = 15
	qingmeiSellCmd       = 16
	qingmeiBrewSteps     = 3
	qingmeiStepDelay     = 1 * time.Second
	// OperateRequest 请求字段编号（activitypb.proto）
	qingmeiClaimParamF   = 103
	qingmeiWineStartF    = 112
	qingmeiWineBrewF     = 113
	qingmeiWineSellF     = 114
	// OperateReply 回包字段编号
	qingmeiClaimReplyF   = 104
	qingmeiPreviewReplyF = 113
	qingmeiBrewReplyF    = 114
	qingmeiSellReplyF    = 115
)

type qingmeiMat struct {
	UID   int64 `json:"uid"`
	Count int64 `json:"count"`
}

// qingmeiMaterialItems 读取背包青梅(41221)材料（对齐 Node getQingmeiWineMaterialItems：需 uid>0 且 count>0，按 uid 排序）
func qingmeiMaterialItems(ctx context.Context, accountID string) []qingmeiMat {
	c, err := clientPool.Get(accountID)
	if err != nil {
		return nil
	}
	rep, err := c.Request(ctx, "gamepb.itempb.ItemService", "Bag", proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		return nil
	}
	br := proto.DecodeBagReply(rep.Body)
	var mats []qingmeiMat
	for _, it := range br.Items {
		if it.ID == qingmeiFruitItemID && it.UID > 0 && it.Count > 0 {
			mats = append(mats, qingmeiMat{UID: it.UID, Count: it.Count})
		}
	}
	sort.Slice(mats, func(i, j int) bool { return mats[i].UID < mats[j].UID })
	return mats
}

// qingmeiBagCount 对齐 Node getBagItemCount：直接求和背包中所有 41221 的 count（不过滤 uid），
// 用于 handleQingmei 的 material.item_count 显示。酿制操作仍用 qingmeiMaterialItems（需 uid>0）。
func qingmeiBagCount(ctx context.Context, accountID string) int64 {
	c, err := clientPool.Get(accountID)
	if err != nil {
		return 0
	}
	rep, err := c.Request(ctx, "gamepb.itempb.ItemService", "Bag", proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		return 0
	}
	br := proto.DecodeBagReply(rep.Body)
	var total int64
	for _, it := range br.Items {
		if it.ID == qingmeiFruitItemID && it.Count > 0 {
			total += it.Count
		}
	}
	return total
}

// qingmeiActIDs 定位青梅活动结点。
// 对齐 Node：claim/wine 直接用写死的活动 ID（Node normalizeQingmeiActivity 用固定 ID，
// 找不到才回退常量，从不靠类型推断）。青梅回包 GetGroupReply 只有 group 树、无顶层 activities，
// 且 claim 结点(2026081201)不在 group 子树里，因此动态按类型发现必然失败——必须用固定 ID。
func qingmeiActIDs(ctx context.Context, accountID string) (rootID, claimID, wineID int64, root *ActivityNode, err error) {
	rootID = qingmeiRootActivityID
	claimID = qingmeiClaimActivityID
	wineID = qingmeiWineActivityID

	// 确认根活动存在（按标题匹配，不过滤日期——对齐 Node 不门控）
	body, e := rpcRequest(ctx, accountID, actSvc, "List", []byte{}, 15*time.Second)
	if e != nil {
		return rootID, claimID, wineID, nil, e
	}
	found := false
	for _, it := range ParseActivityList(body) {
		if it.Title == "青酿换万金" {
			rootID = it.ID
			found = true
			break
		}
	}
	if !found {
		return rootID, claimID, wineID, nil, fmt.Errorf("青梅活动（青酿换万金）未找到")
	}

	// 拉分组树（用于解析领种子状态、酿制结点标题等）
	gb := proto.NewBuilder()
	gb.FieldInt64(1, rootID)
	gb.FieldString(2, "")
	ck := actGroupCacheKey(accountID, rootID)
	gbody, ok := actCacheGet(ck, 30*time.Second)
	if !ok {
		gbody, e = rpcRequest(ctx, accountID, actSvc, "GetGroup", gb.Bytes(), 20*time.Second)
		if e != nil {
			return rootID, claimID, wineID, nil, e
		}
		actCacheSet(ck, gbody, 30*time.Second)
	}
	root = ParseActivityGroup(gbody)
	return rootID, claimID, wineID, root, nil
}

// qingmeiOperate 组装青梅 Operate 请求（id/cmd + 可选扩展字段），返回原始回包 body
func qingmeiOperate(ctx context.Context, accountID string, actID, cmd int64, extField int, extBody []byte) ([]byte, error) {
	b := proto.NewBuilder()
	b.FieldInt64(1, actID)
	b.FieldInt64(2, cmd)
	if extField > 0 && extBody != nil {
		b.FieldBytes(extField, extBody)
	}
	return rpcRequest(ctx, accountID, actSvc, "Operate", b.Bytes(), 20*time.Second)
}

// subFieldBytes 取回包中指定字段的嵌套消息 bytes（容错）
func subFieldBytes(body []byte, field int) []byte {
	defer func() { _ = recover() }()
	fs := readActFields(body)
	return actBytes(fs, field)
}

func handleQingmei(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	rootID, claimID, wineID, root, err := qingmeiActIDs(ctx, accountID)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	// 复用 qingmeiActIDs 已拉取的 group 根节点解析状态（避免二次 GetGroup）
	var claimStatus int64 = 0
	wineTitle := "青酿换万金"
	startTime := int64(0)
	endTime := int64(0)
	if root != nil {
		var walk func(n *ActivityNode)
		walk = func(n *ActivityNode) {
			if n == nil || n.Info == nil {
				return
			}
			if n.Info.ID == claimID {
				claimStatus = n.Info.Status
			}
			if n.Info.ID == wineID {
				if n.Info.Title != "" {
					wineTitle = n.Info.Title
				}
				startTime = n.Info.StartTime
				endTime = n.Info.EndTime
			}
			for _, ch := range n.Children {
				walk(ch)
			}
		}
		walk(root)
	}
	// 对齐 Node getBagItemCount：显示数量直接求和所有 41221（不过滤 uid）；
	// 酿制操作仍用 qingmeiMaterialItems（需 uid>0 作为实例 ID）。
	total := qingmeiBagCount(ctx, accountID)
	claimed := claimStatus == 3 || qingmeiClaimedToday(accountID)
	claimable := !claimed

	seedName := itemDisplayName(qingmeiSeedItemID)
	if seedName == "" {
		seedName = "青梅种子"
	}
	fruitName := itemDisplayName(qingmeiFruitItemID)
	if fruitName == "" {
		fruitName = "青梅"
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"title": "青酿换万金",
		"activity": map[string]interface{}{
			"activity_id": rootID,
			"claim_activity_id": claimID,
			"wine_activity_id": wineID,
			"wine_title": wineTitle,
			"start_time": startTime,
			"end_time": endTime,
			"status": claimStatus, "claimed": claimed, "claimable": claimable,
		},
		"reward": map[string]interface{}{
			"item_id": qingmeiSeedItemID, "item_count": qingmeiSeedReward,
			"item_name": seedName, "image": "",
		},
		"material": map[string]interface{}{
			"item_id": qingmeiFruitItemID, "item_count": total,
			"item_name": fruitName, "image": "",
		},
	})
}

func handleQingmeiClaim(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rootID, claimID, _, _, err := qingmeiActIDs(ctx, accountID)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	// 调试：允许 query 覆盖 cmd 与 type。默认 cmd=4、type=3（本期实测；Node 上期为 type=2）
	var cmd int64 = qingmeiClaimCmd
	if v := r.URL.Query().Get("cmd"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			cmd = n
		}
	}
	tp := int32(3)
	if v := r.URL.Query().Get("type"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 32); e == nil {
			tp = int32(n)
		}
	}
	sub := proto.NewBuilder()
	sub.FieldInt32(1, tp) // QingmeiClaimParams.type
	body, err := qingmeiOperate(ctx, accountID, claimID, cmd, qingmeiClaimParamF, sub.Bytes())
	if err != nil {
		es := actErrMsg(err)
		// 今日已领过：标记并返回成功语义（对齐 Node isAlreadyClaimedError → markQingmeiClaimedToday）
		if strings.Contains(es, "已领取") {
			qingmeiMarkClaimed(accountID)
			writeJSONMap(w, "ok", true, "account", accountID, "claimed_count", int64(0), "already_claimed", true, "reward_item_id", qingmeiSeedItemID)
			return
		}
		writeJSONMap(w, "ok", false, "error", es)
		return
	}
	// 解析礼包物品（qingmei_claim=104 -> items=1）
	var items []map[string]int64
	if subRaw := subFieldBytes(body, qingmeiClaimReplyF); len(subRaw) > 0 {
		sfs := readActFields(subRaw)
		for _, itRaw := range actBytesAll(sfs, 1) {
			it := readActFields(itRaw)
			items = append(items, map[string]int64{
				"item_id": actNum(it, 1), "count": actNum(it, 2),
			})
		}
	}
	claimed := int64(0)
	for _, it := range items {
		claimed += it["count"]
	}
	if claimed == 0 {
		claimed = qingmeiSeedReward
	}
	// 领种子后：清 group 缓存并标记今日已领，让状态/按钮立即反映
	actCacheDel(actGroupCacheKey(accountID, rootID))
	qingmeiMarkClaimed(accountID)
	writeJSONMap(w, "ok", true, "account", accountID, "claimed_count", claimed, "reward_item_id", qingmeiSeedItemID, "items", items)
}

func handleQingmeiWine(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	_, _, wineID, _, err := qingmeiActIDs(ctx, accountID)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	mats := qingmeiMaterialItems(ctx, accountID)
	beforeTotal := int64(0)
	for _, m := range mats {
		beforeTotal += m.Count
	}
	if beforeTotal <= 0 {
		writeJSONMap(w, "ok", false, "error", "青梅不足，无法酿制")
		return
	}
	// 组装 qingmei_wine_start 材料（items=1 -> corepb.Item{id=实例uid, count}，对齐 Node map id:item.uid）
	startSub := proto.NewBuilder()
	for _, m := range mats {
		it := proto.NewBuilder()
		it.FieldInt64(1, m.UID)
		it.FieldInt64(2, m.Count)
		startSub.FieldBytes(1, it.Bytes())
	}
	previewPrice := int64(0)
	previewWarning := ""
	previewBody, pErr := qingmeiOperate(ctx, accountID, wineID, qingmeiPreviewCmd, qingmeiWineStartF, startSub.Bytes())
	if pErr != nil {
		previewWarning = actErrMsg(pErr)
	} else if subRaw := subFieldBytes(previewBody, qingmeiPreviewReplyF); len(subRaw) > 0 {
		previewPrice = actNum(readActFields(subRaw), 1)
	}
	time.Sleep(qingmeiStepDelay)

	// 精酿多次（对齐 Node brewSteps=3，每次间 delay）
	type brewRes struct {
		WineType int64 `json:"wine_type"`
		Cost     int64 `json:"cost"`
		Price    int64 `json:"price"`
		CanDouble bool `json:"can_double"`
	}
	var brews []*brewRes
	for i := 0; i < qingmeiBrewSteps; i++ {
		brewBody, bErr := qingmeiOperate(ctx, accountID, wineID, qingmeiBrewCmd, qingmeiWineBrewF, []byte{})
		if bErr != nil {
			es := actErrMsg(bErr)
			if i == 0 {
				// 首次失败：可能是未打开酿制，重试 preview+brew
				rs := proto.NewBuilder()
				for _, m := range mats {
					it := proto.NewBuilder()
					it.FieldInt64(1, m.UID)
					it.FieldInt64(2, m.Count)
					rs.FieldBytes(1, it.Bytes())
				}
				_, _ = qingmeiOperate(ctx, accountID, wineID, qingmeiPreviewCmd, qingmeiWineStartF, rs.Bytes())
				time.Sleep(qingmeiStepDelay)
				brewBody, bErr = qingmeiOperate(ctx, accountID, wineID, qingmeiBrewCmd, qingmeiWineBrewF, []byte{})
				if bErr != nil {
					writeJSONMap(w, "ok", false, "error", "青梅酿精酿失败: "+actErrMsg(bErr))
					return
				}
			} else {
				writeJSONMap(w, "ok", false, "error", "青梅酿精酿失败: "+es)
				return
			}
		}
		var br brewRes
		if subRaw := subFieldBytes(brewBody, qingmeiBrewReplyF); len(subRaw) > 0 {
			bf := readActFields(subRaw)
			br.WineType = actNum(bf, 1)
			br.Cost = actNum(bf, 2)
			br.Price = actNum(bf, 3)
			br.CanDouble = actNum(bf, 4) != 0
		}
		brews = append(brews, &br)
		time.Sleep(qingmeiStepDelay)
	}
	finalBrew := brews[len(brews)-1]

	// 分享翻倍（对齐 Node brewAndSellQingmeiWine：精酿结果可翻倍才做分享上报，成功则 multiple=2 出售）
	shared := false
	if finalBrew.CanDouble {
		// 1) CheckCanShare：判断当前是否可分享
		checkBody, cErr := rpcRequest(ctx, accountID, shareSvc, "CheckCanShare", []byte{}, 20*time.Second)
		if cErr != nil {
			writeJSONMap(w, "ok", false, "error", "青梅酿分享翻倍失败: "+actErrMsg(cErr))
			return
		}
		if actNum(readActFields(checkBody), 1) == 0 {
			writeJSONMap(w, "ok", false, "error", "当前不可分享，无法执行青梅酿售卖翻倍")
			return
		}
		// 2) ReportShare：上报已分享 {shared:true}
		repB := proto.NewBuilder()
		repB.FieldBool(1, true)
		repBody := repB.Bytes()
		reportBody, rErr := rpcRequest(ctx, accountID, shareSvc, "ReportShare", repBody, 20*time.Second)
		if rErr != nil {
			writeJSONMap(w, "ok", false, "error", "青梅酿分享上报失败: "+actErrMsg(rErr))
			return
		}
		// 仅当返回体显式 success=false 才算失败（对齐 Node success !== false）
		for _, f := range readActFields(reportBody) {
			if f.No == 1 && f.Wire == 0 && f.Varint == 0 {
				writeJSONMap(w, "ok", false, "error", "青梅酿分享上报失败")
				return
			}
		}
		shared = true
		time.Sleep(qingmeiStepDelay)
	}
	sellMultiple := int32(1)
	if shared {
		sellMultiple = 2
	}
	// 出售（分享成功 multiple=2 翻倍；否则 multiple=1）
	sellSub := proto.NewBuilder()
	sellSub.FieldInt32(1, sellMultiple)
	sellBody, sErr := qingmeiOperate(ctx, accountID, wineID, qingmeiSellCmd, qingmeiWineSellF, sellSub.Bytes())
	if sErr != nil {
		writeJSONMap(w, "ok", false, "error", "青梅酿售卖失败: "+actErrMsg(sErr))
		return
	}
	sell := map[string]int64{"gold": 0, "multiple": 1}
	if subRaw := subFieldBytes(sellBody, qingmeiSellReplyF); len(subRaw) > 0 {
		sf := readActFields(subRaw)
		sell["multiple"] = actNum(sf, 1)
		sell["gold"] = actNum(sf, 2)
	}
	if sell["gold"] <= 0 {
		writeJSONMap(w, "ok", false, "error", "售卖未返回金币收益，请稍后刷新活动状态")
		return
	}
	afterTotal := int64(0)
	for _, m := range qingmeiMaterialItems(ctx, accountID) {
		afterTotal += m.Count
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"before_material": beforeTotal, "after_material": afterTotal,
		"consumed": max64(0, beforeTotal-afterTotal),
		"brew_steps": len(brews), "brews": brews,
		"preview": map[string]int64{"price": previewPrice}, "preview_warning": previewWarning,
		"wine": finalBrew,
		"shared": shared,
		"sell": sell,
	})
}

func max64(a, b int64) int64 { if a > b { return a }; return b }

// ===== 活动 GetGroup 短缓存 =====
// 游戏 GetGroup 返回整棵活动树，量大且相对稳定；每次切页/取状态都重拉容易把 RPC 链路拖垮导致超时。
// 这里做短 TTL 内存缓存（仅缓存 GetGroup 的原始 body）。List 保持实时，仍支持前端「获取新活动」。
var (
	actCacheMu sync.Mutex
	actCache   = map[string]actCacheItem{}
)

type actCacheItem struct {
	data []byte
	exp  time.Time
}

func actGroupCacheKey(accountID string, actID int64) string {
	return "actgroup:" + accountID + ":" + strconv.FormatInt(actID, 10)
}

func actCacheGet(key string, ttl time.Duration) ([]byte, bool) {
	actCacheMu.Lock()
	defer actCacheMu.Unlock()
	if it, ok := actCache[key]; ok && time.Now().Before(it.exp) {
		return it.data, true
	}
	delete(actCache, key)
	return nil, false
}

func actCacheSet(key string, data []byte, ttl time.Duration) {
	actCacheMu.Lock()
	defer actCacheMu.Unlock()
	actCache[key] = actCacheItem{data: data, exp: time.Now().Add(ttl)}
}

func actCacheDel(key string) {
	actCacheMu.Lock()
	defer actCacheMu.Unlock()
	delete(actCache, key)
}

// ===== 青梅每日领种子内存标记 =====
// 对齐 Node activity.js qingmeiClaimedDateByAccount：服务端 status 领后不变(0)，无法据活动树判断当日是否已领。
// 这里用内存记录「账号今日已领」，重启丢失（与 Node 一致，可接受）。
var (
	qingmeiClaimedMu   sync.Mutex
	qingmeiClaimedDate = map[string]string{} // accountID -> YYYYMMDD
)

func qingmeiTodayKey() string { return time.Now().Format("20060102") }

func qingmeiClaimedToday(accountID string) bool {
	qingmeiClaimedMu.Lock()
	defer qingmeiClaimedMu.Unlock()
	return qingmeiClaimedDate[accountID] == qingmeiTodayKey()
}

func qingmeiMarkClaimed(accountID string) {
	qingmeiClaimedMu.Lock()
	defer qingmeiClaimedMu.Unlock()
	qingmeiClaimedDate[accountID] = qingmeiTodayKey()
}

// ===== 临时调试：鹊桥 Operate cmd 探测（TODO: 探测完成后删除） =====
// GET /api/debug/act_operate?accountId=X&id=2026081802&cmd=N
// 向指定账号发送 ActivityService.Operate(id,cmd) 空扩展请求，返回原始回包字段，用于确定鹊桥灵露/筑桥/香囊 cmd。
func handleDebugActOperate(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	cmd, _ := strconv.ParseInt(r.URL.Query().Get("cmd"), 10, 64)
	if id <= 0 {
		writeError(w, 400, "missing/invalid id")
		return
	}
	b := proto.NewBuilder()
	b.FieldInt64(1, id)
	b.FieldInt64(2, cmd)
	// 可选扩展参数（灵露喷洒 land_id 等）
	if landID, _ := strconv.ParseInt(r.URL.Query().Get("land_id"), 10, 64); landID > 0 {
		hostGid, _ := strconv.ParseInt(r.URL.Query().Get("host_gid"), 10, 64)
		sf, _ := strconv.Atoi(r.URL.Query().Get("ext_field"))
		if sf == 0 {
			sf = 101
		}
		sub := proto.NewBuilder()
		sub.FieldInt64(1, landID)
		if hostGid > 0 {
			sub.FieldInt64(2, hostGid)
		}
		b.FieldMessage(sf, sub.Bytes())
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, actSvc, "Operate", b.Bytes(), 20*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err), "id", id, "cmd", cmd)
		return
	}
	fields := []map[string]interface{}{}
	for _, f := range readActFields(body) {
		switch f.Wire {
		case 0:
			fields = append(fields, map[string]interface{}{"field": f.No, "varint": f.Varint})
		case 2:
			if len(f.Bytes) <= 4096 {
				fields = append(fields, map[string]interface{}{"field": f.No, "bytesLen": len(f.Bytes), "str": string(f.Bytes)})
			} else {
				fields = append(fields, map[string]interface{}{"field": f.No, "bytesLen": len(f.Bytes)})
			}
		default:
			fields = append(fields, map[string]interface{}{"field": f.No, "wire": f.Wire})
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "id": id, "cmd": cmd, "hex": fmt.Sprintf("%X", body), "fields": fields})
}

// ===== 临时调试：PlantService 方法探测（鹊桥灵露喷洒方法名定位，探测后删除） =====
// GET /api/debug/plant_rpc?accountId=X&method=<PlantService方法>&land_id=..&host_gid=..&item_id=..
// 参数布局：land_ids=1 / host_gid=2 / item_id=3（可先试 Fertilize 类 {land_ids=1,item_id=2}）
func handleDebugPlantRPC(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	method := r.URL.Query().Get("method")
	layout := r.URL.Query().Get("layout")
	landID, _ := strconv.ParseInt(r.URL.Query().Get("land_id"), 10, 64)
	hostGid, _ := strconv.ParseInt(r.URL.Query().Get("host_gid"), 10, 64)
	itemID, _ := strconv.ParseInt(r.URL.Query().Get("item_id"), 10, 64)
	if method == "" {
		writeError(w, 400, "missing method")
		return
	}
	b := proto.NewBuilder()
	if layout == "putinsects" {
		// PutInsects/PutWeeds: host_gid=1, land_ids=2
		if hostGid > 0 {
			b.FieldInt64(1, hostGid)
		}
		if landID > 0 {
			b.FieldInt64(2, landID)
		}
	} else {
		if landID > 0 {
			b.FieldInt64(1, landID)
		}
		if hostGid > 0 {
			b.FieldInt64(2, hostGid)
		}
		if itemID > 0 {
			// Feriliize 布局：item 放 field2（fertilizer_id），host 不占 field2 时
			if layout == "fertilize" {
				// 重建：land_ids=1, fertilizer_id=2
				b = proto.NewBuilder()
				b.FieldInt64(1, landID)
				b.FieldInt64(2, itemID)
			} else {
				b.FieldInt64(3, itemID)
			}
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, plantService, method, b.Bytes(), 20*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", "GRPC:"+err.Error(), "method", method)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "method": method, "bodyLen": len(body), "hex": fmt.Sprintf("%X", body)})
}

// ===== 临时调试：背包物品列表（鹊桥物品ID/数量确认，探测后删除） ======
func handleDebugBagDump(w http.ResponseWriter, r *http.Request) {
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
	items := []map[string]interface{}{}
	total := int64(0)
	for _, it := range br.Items {
		if it.Count > 0 {
			items = append(items, map[string]interface{}{"id": it.ID, "count": it.Count})
			total += it.Count
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "totalKinds": len(items), "items": items})
}

// ===== 鹊桥寄情：首页动态数据（鹊羽/鹊羽灵露/进度/香囊）=====
// 鹊羽灵露 = 背包物品 301103；鹊羽当前未获得（来源待 Activity 状态 cmd 确认）；筑桥进度/香囊待确认。
func handleQiXiStatus(w http.ResponseWriter, r *http.Request) {
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
	var lu, feather, sachet int64
	for _, it := range br.Items {
		switch it.ID {
		case 301103: // 鹊羽灵露
			lu = it.Count
		case 1024: // 鹊羽（2026-08-17 抓包 ItemNotify/Bag 实锤：item 1024 数量 1→2→3 = 喷洒/收菜所得）
			feather = it.Count
		case 1025: // 鹊羽香囊（GetGroup field112 奖励物品，用户确认）
			sachet = it.Count
		}
	}
	// 筑桥三档奖励（2026-08-17 抓包 GetGroup field112 确认：每档消耗 N 鹊羽 → 获得奖励）
	// 档位 flag（GetGroup 响应 112.2.N.4：2=已领取/1=未领）→ 决定"下一可筑档"
	tierFlags := map[int64]int64{}
	if grep, e2 := c.Request(ctx, actSvc, "GetGroup", func() []byte {
		b := proto.NewBuilder()
		b.FieldInt64(1, 2026081800)
		return b.Bytes()
	}(), 12*time.Second); e2 == nil {
		tierFlags = parseQiXiTierFlags(grep.Body)
	}
	tiers := []map[string]interface{}{
		{
			"consume": int64(30), // 第一档：消耗 30 鹊羽
			"rewards": []map[string]interface{}{
				{"id": int64(1025), "name": "鹊羽香囊", "count": int64(5)},
				{"id": int64(101325), "name": "鹊桥寄情礼包一", "count": int64(1)},
			},
		},
		{
			"consume": int64(50), // 第二档：消耗 50 鹊羽
			"rewards": []map[string]interface{}{
				{"id": int64(101326), "name": "鹊桥寄情礼包二", "count": int64(1)},
				{"id": int64(1025), "name": "鹊羽香囊", "count": int64(5)},
			},
		},
		{
			"consume": int64(77), // 第三档：消耗 77 鹊羽
			"rewards": []map[string]interface{}{
				{"id": int64(401004), "name": "鹊桥寄情铭牌", "count": int64(1)},
			},
		},
	}
	// 每档回填已领状态（flag=2 已领），并计算下一可筑档
	nextConsume := int64(0)
	nextIndex := -1
	claimedAll := true
	for i, t := range tiers {
		claim := tierFlags[int64(i+1)]
		t["claimed"] = claim == 2
		if claim != 2 {
			claimedAll = false
			if nextIndex < 0 {
				nextConsume = t["consume"].(int64)
				nextIndex = i
			}
		}
	}
	bridgeTarget := nextConsume // 下一可筑档的鹊羽门槛（档位独立消耗，非累计）
	if bridgeTarget == 0 {
		bridgeTarget = int64(77)
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"data": map[string]interface{}{
			"feather":      feather, // 鹊羽（背包 1024）
			"luStock":      lu,      // 鹊羽灵露（背包 301103）
			"bridgeDone":   0,       // 待 Operate cmd 确认
			"bridgeMax":    len(tiers),
			"bridgeTarget": bridgeTarget, // 下一可筑档消耗（档位独立门槛）
			"claimedAll":   claimedAll,   // 是否三档全领
			"sachet":       sachet,       // 鹊羽香囊（背包 1025）
			"luLimit":      nil,          // null=待接口确认
			"tiers":        tiers,        // 筑桥三档奖励（消耗鹊羽 → 奖励，含 claimed）
		},
	})
}

// parseQiXiTierFlags 从 GetGroup 响应提取 1801 节点 field112 各档 flag（2=已领/1=未领）。
// 导航对齐 ParseActivityGroup（线上已验证）：rpcRequest 返回的 body 已是响应体(信封 field2)，
// 响应体.field1 = root node(1800)，root.field2 每个直接是子节点(1801/1802)，
// 节点.field1(info).field1 = id，节点.field112(配置).field2 repeated = tiers{1:档号, 4:flag}
func parseQiXiTierFlags(body []byte) map[int64]int64 {
	out := map[int64]int64{}
	get := func(buf []byte, no int) []byte {
		for _, f := range readActFields(buf) {
			if f.No == no && f.Wire == 2 {
				return f.Bytes
			}
		}
		return nil
	}
	grp := get(body, 1) // 响应体.field1 = root node（1800）
	if grp == nil {
		return out
	}
	for _, child := range actBytesAll(readActFields(grp), 2) { // root.field2 repeated = 子节点
		info := get(child, 1)
		nid := int64(0)
		for _, nf := range readActFields(info) {
			if nf.No == 1 && nf.Wire == 0 {
				nid = nf.Varint
			}
		}
		if nid != 2026081801 {
			continue
		}
		cfg := get(child, 112)
		if cfg == nil {
			return out
		}
		for _, tf := range actBytesAll(readActFields(cfg), 2) { // 配置.field2 repeated = 各档
			tierNo, flag := int64(0), int64(1)
			for _, tif := range readActFields(tf) {
				switch {
				case tif.No == 1 && tif.Wire == 0:
					tierNo = tif.Varint
				case tif.No == 4 && tif.Wire == 0:
					flag = tif.Varint
				}
			}
			if tierNo > 0 {
				out[tierNo] = flag
			}
		}
	}
	return out
}

// ===== 鹊桥寄情：灵露喷洒（抓包确认 = ItemService.Use，UseRequest{item_id=301103,count=1,land_ids=[地块]}）=====
// POST /api/activity/qixi/spray  body: {"accountId":"...","hostGid":123,"landIds":[1,2]}
// hostGid>0 喷好友地块（AllLands(hostGid)）；不传则喷自己地块；landIds 不传则自动选全部有作物地块；每块 +1 鹊羽。
func handleQiXiSpray(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	var req struct {
		AccountID string  `json:"accountId"`
		HostGID   int64   `json:"hostGid"`
		LandIDs   []int64 `json:"landIds"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.AccountID != "" {
		accountID = req.AccountID
	}
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", err.Error())
		return
	}
	// 好友喷洒必须先进入对方农场（抓包帧序列铁证：VisitService.Enter → ItemService.Use）
	if req.HostGID > 0 {
		if _, _, err := enterFriendFarm(c, req.HostGID, 2, ""); err != nil {
			writeJSONMap(w, "ok", false, "error", "Enter:"+err.Error())
			return
		}
		defer leaveFriendFarm(c, req.HostGID)
	}
	// 拉目标地块（hostGid>0 拉好友地块，否则自己的）
	rep, err := c.Request(ctx, plantService, "AllLands", proto.EncodeAllLandsRequest(req.HostGID), 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", "GRPC:"+err.Error())
		return
	}
	allLandsHex := fmt.Sprintf("%X", rep.Body) // 临时调试
	lands := proto.DecodeAllLandsReply(rep.Body).Lands
	want := map[int64]bool{}
	for _, id := range req.LandIDs {
		want[id] = true
	}
	var selected []int64
	var landDebug []map[string]interface{}
	for _, l := range lands {
		hasCrop := l.Plant != nil && len(l.Plant.Phases) > 0
		landDebug = append(landDebug, map[string]interface{}{
			"id": l.ID, "size": l.LandSize, "master": l.MasterLandID, "slaves": l.SlaveLandIDs, "crop": hasCrop,
		})
		if !hasCrop {
			continue
		}
		if len(want) > 0 && !want[l.ID] {
			continue
		}
		// 合种地块（LandSize>1+SlaveLandIDs）：master+slaves 一起喷洒（对齐抓包合种喷洒 land_ids 多地块）
		if l.LandSize > 1 && len(l.SlaveLandIDs) > 0 {
			selected = append(selected, l.ID)
			selected = append(selected, l.SlaveLandIDs...)
		} else {
			selected = append(selected, l.ID)
		}
	}
	if len(selected) == 0 {
		writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "data": map[string]interface{}{"sprayed": []int64{}, "sprayCount": 0, "featherGain": 0, "errors": []string{}, "msg": "无可喷洒地块（无作物或未指定）"}})
		return
	}
	// 逐块喷洒：每块一次 Use，消耗 1 瓶灵露（301103），活动规则恒得 1 根鹊羽（1024）
	// 布局：{field1=corepb.Item{item_id=1,count=2,uid=6}, field2=land_ids}
	// （Use 响应 field1 回显 corepb.Item 格式，推断请求 item 也是 corepb.Item 子消息）
	luUID := int64(0)
	if brep, e := c.Request(ctx, "gamepb.itempb.ItemService", "Bag", proto.EncodeBagRequest(), 12*time.Second); e == nil {
		for _, it := range proto.DecodeBagReply(brep.Body).Items {
			if it.ID == 301103 && it.UID > 0 {
				luUID = it.UID
				break
			}
		}
	}
	var sprayed []int64
	var errs []string
	// 喷洒真实结构（2026-08-17 tsdk.wasm 解密抓包明文实锤）：单次 Use，无 land_ids
	// UseRequest = { field1 (LEN): item{301103,1,uid}, field2 (LEN): {host_gid, 标记9} }
	item := proto.NewBuilder()
	item.FieldInt64Always(1, 301103)
	item.FieldInt64Always(2, 1)
	if luUID > 0 {
		item.FieldInt64(6, luUID)
	}
	sub := proto.NewBuilder()
	sub.FieldInt64Always(1, req.HostGID)
	sub.FieldInt64Always(2, 9) // 喷洒标记（抓包第一次=9）
	ub := proto.NewBuilder()
	ub.FieldMessage(1, item.Bytes())
	ub.FieldMessage(2, sub.Bytes())
	sprayHex := fmt.Sprintf("%X", ub.Bytes())
	if _, e2 := c.Request(ctx, "gamepb.itempb.ItemService", "Use", ub.Bytes(), 12*time.Second); e2 != nil {
		errs = append(errs, fmt.Sprintf("spray:%v(hex:%s)", e2, sprayHex))
	} else {
		sprayed = append(sprayed, 0) // 无 land 概念，成功即完成
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"data": map[string]interface{}{
			"sprayed":     sprayed, // 成功喷洒的地块
			"sprayCount":  len(sprayed),
			"featherGain": len(sprayed), // 每块 +1 鹊羽
			"errors":      errs,
			"luUID":       luUID, // 临时调试：灵露实例 uid
			"selected":    selected,
			"lands":       landDebug, // 临时调试：地块结构
			"allLandsHex": allLandsHex, // 临时调试：AllLands 原始字节
		},
	})
}

// ===== 鹊桥寄情：筑建鹊桥（抓包 2026-08-17 实锤：ActivityService.Operate{id=2026081801, cmd=25}）=====
// POST /api/activity/qixi/bridge  body: {"accountId":"..."}
// 抓包响应 1487 明文：.2.1=2026081801、.2.2=25（请求回显）、.2.126=发放奖励
// （鹊羽香囊1025×5 + 8小时化肥80003×4 + 点券1002×200）、.2.3.112.2.N.4=档位 flag(2=已领取)
// 线上验证：Agoni 重复调用返回"该步骤奖励已领取"= 结构正确。
func handleQiXiBridge(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	var req struct {
		AccountID string `json:"accountId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.AccountID != "" {
		accountID = req.AccountID
	}
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	b := proto.NewBuilder()
	b.FieldInt64(1, 2026081801) // 活动节点 id（抓包响应回显）
	b.FieldInt64(2, 25)         // cmd=25 筑桥
	body, err := rpcRequest(ctx, accountID, actSvc, "Operate", b.Bytes(), 20*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	rewards := parseActRewardField(body, 126)
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"data": map[string]interface{}{
			"rewards": rewards,
			"msg":     "筑桥成功",
		},
	})
}

// ===== 鹊桥寄情：赠送鹊羽香囊（流程对齐喷洒：先 Enter 好友农场再 Operate；cmd=26 待真机验证）=====
// POST /api/activity/qixi/gift  body: {"accountId":"...","hostGid":123}
// 玩法（tips 第 6 条）：活动期间可将鹊羽香囊赠送给好友。
func handleQiXiGift(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	var req struct {
		AccountID string `json:"accountId"`
		HostGID   int64  `json:"hostGid"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.AccountID != "" {
		accountID = req.AccountID
	}
	if accountID == "" || req.HostGID <= 0 {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId/hostGid")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", err.Error())
		return
	}
	// 先 Enter 好友农场（对齐抓包喷洒帧序列：Enter → 操作）
	if _, _, err := enterFriendFarm(c, req.HostGID, 2, ""); err != nil {
		writeJSONMap(w, "ok", false, "error", "Enter:"+err.Error())
		return
	}
	defer leaveFriendFarm(c, req.HostGID)
	b := proto.NewBuilder()
	b.FieldInt64(1, 2026081801) // 活动节点 id
	b.FieldInt64(2, 26)         // cmd=26 赠送香囊（待真机抓包确认，实测后可改）
	// Operate ext 结构（筑桥明文实锤 field125={field1:0}）：送香囊 ext={field1: 目标好友gid}
	ext := proto.NewBuilder()
	ext.FieldInt64Always(1, req.HostGID)
	b.FieldMessage(125, ext.Bytes())
	body, err := c.Request(ctx, actSvc, "Operate", b.Bytes(), 20*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	rewards := parseActRewardField(body.Body, 126)
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"data": map[string]interface{}{
			"giftTo": req.HostGID,
			"msg":    "香囊赠送成功",
			"rewards": rewards,
		},
	})
}

// parseActRewardField 从 Operate/GetGroup 响应体取 field126（或指定号）奖励块：
// 响应结构：field2(响应体) → field126(奖励MSG) → field2 repeated items{1:id,2:count,6:uid}
func parseActRewardField(body []byte, targetField int) []map[string]interface{} {
	var out []map[string]interface{}
	get := func(buf []byte, no int) []byte {
		for _, f := range readActFields(buf) {
			if f.No == no && f.Wire == 2 {
				return f.Bytes
			}
		}
		return nil
	}
	body2 := get(body, 2) // .2 响应体
	if body2 == nil {
		return out
	}
	rew := get(body2, targetField) // .2.126
	if rew == nil {
		return out
	}
	for _, f := range readActFields(rew) {
		if f.No == 2 && f.Wire == 2 { // 每个奖励 item
			iid, cnt := int64(0), int64(0)
			for _, it := range readActFields(f.Bytes) {
				switch {
				case it.No == 1 && it.Wire == 0:
					iid = it.Varint
				case it.No == 2 && it.Wire == 0:
					cnt = it.Varint
				}
			}
			if iid > 0 {
				out = append(out, map[string]interface{}{"id": iid, "name": qixiItemName(iid), "count": cnt})
			}
		}
	}
	return out
}

// qixiItemName 鹊桥物品名映射（2026-08-17 用户确认 + 抓包实锤）
func qixiItemName(id int64) string {
	switch id {
	case 1024:
		return "鹊羽"
	case 1025:
		return "鹊羽香囊"
	case 1002:
		return "点券"
	case 80001, 80002, 80003, 80004:
		return "化肥"
	case 101325:
		return "鹊桥寄情礼包一"
	case 101326:
		return "鹊桥寄情礼包二"
	case 401004:
		return "鹊桥寄情铭牌"
	case 301103:
		return "鹊羽灵露"
	}
	return fmt.Sprintf("物品#%d", id)
}

// ===== 临时调试：ItemService.Use 探测（灵露喷洒=放黄金虫=使用物品到地块）=====
// GET /api/debug/item_use?accountId=X&item=..&count=..&land_id=..&land_field=3&shape=nested|flat
func handleDebugItemUse(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	item, _ := strconv.ParseInt(r.URL.Query().Get("item"), 10, 64)
	count, _ := strconv.ParseInt(r.URL.Query().Get("count"), 10, 64)
	landID, _ := strconv.ParseInt(r.URL.Query().Get("land_id"), 10, 64)
	hostGID, _ := strconv.ParseInt(r.URL.Query().Get("host_gid"), 10, 64)
	lfield, _ := strconv.Atoi(r.URL.Query().Get("land_field"))
	if lfield == 0 {
		lfield = 3
	}
	hfield, _ := strconv.Atoi(r.URL.Query().Get("host_field"))
	if hfield == 0 {
		hfield = 4
	}
	shape := r.URL.Query().Get("shape")
	layout := r.URL.Query().Get("layout")
	uid, _ := strconv.ParseInt(r.URL.Query().Get("uid"), 10, 64)
	if item <= 0 {
		writeError(w, 400, "missing item")
		return
	}
	if count <= 0 {
		count = 1
	}
	var body []byte
	if layout == "social" { // PutSocialItem 风格平铺: host_gid=1, land_ids=2, item_id=3, count=4
		b := proto.NewBuilder()
		if hostGID > 0 {
			b.FieldInt64(1, hostGID)
		}
		if landID > 0 {
			b.FieldInt64(2, landID)
		}
		b.FieldInt64Always(3, item)
		b.FieldInt64Always(4, count)
		body = b.Bytes()
	} else if shape == "flat" {
		b := proto.NewBuilder()
		b.FieldInt64(1, item)
		b.FieldInt64(2, count)
		if landID > 0 {
			b.FieldInt64(lfield, landID)
		}
		if hostGID > 0 {
			b.FieldInt64(hfield, hostGID)
		}
		if uid > 0 {
			b.FieldInt64(6, uid)
		}
		body = b.Bytes()
	} else { // nested: 外层 field1=子消息{item,count,land@lfield,host@hfield,uid@6}
		sub := proto.NewBuilder()
		sub.FieldInt64Always(1, item)
		sub.FieldInt64Always(2, count)
		if landID > 0 {
			sub.FieldInt64(lfield, landID)
		}
		if hostGID > 0 {
			sub.FieldInt64(hfield, hostGID)
		}
		if uid > 0 {
			sub.FieldInt64(6, uid)
		}
		b := proto.NewBuilder()
		b.FieldMessage(1, sub.Bytes())
		body = b.Bytes()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", err.Error())
		return
	}
	rep, err := c.Request(ctx, "gamepb.itempb.ItemService", "Use", body, 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", "GRPC:"+err.Error(), "hex", fmt.Sprintf("%X", body))
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "hex": fmt.Sprintf("%X", body), "bodyLen": len(rep.Body), "respHex": fmt.Sprintf("%X", rep.Body)})
}