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

// qingmeiActIDs 动态定位当前在期的青梅活动组根、领种子结点(claim,type4)、酿制结点(wine,type12 或 payload 含 QingMei)
func qingmeiActIDs(ctx context.Context, accountID string) (rootID, claimID, wineID int64, root *ActivityNode, err error) {
	if rootID == 0 {
		body, e := rpcRequest(ctx, accountID, actSvc, "List", []byte{}, 15*time.Second)
		if e != nil {
			return 0, 0, 0, nil, e
		}
		now := time.Now().Unix()
		for _, it := range ParseActivityList(body) {
			if it.ID%100 != 0 || it.Title != "青酿换万金" {
				continue
			}
			// 有任一在期子活动即视为在期
			on := it.StartTime > 0 && it.EndTime > 0 && it.StartTime <= now && now <= it.EndTime
			if on {
				rootID = it.ID
				break
			}
		}
		if rootID == 0 {
			return 0, 0, 0, nil, fmt.Errorf("青梅活动（青酿换万金）当前未在进行中")
		}
	}
	gb := proto.NewBuilder()
	gb.FieldInt64(1, rootID)
	gb.FieldString(2, "")
	ck := actGroupCacheKey(accountID, rootID)
	gbody, ok := actCacheGet(ck, 30*time.Second)
	if !ok {
		var e error
		gbody, e = rpcRequest(ctx, accountID, actSvc, "GetGroup", gb.Bytes(), 20*time.Second)
		if e != nil {
			return 0, 0, 0, nil, e
		}
		actCacheSet(ck, gbody, 30*time.Second)
	}
	root = ParseActivityGroup(gbody)
	if root == nil {
		return rootID, 0, 0, root, fmt.Errorf("青梅活动分组解析失败")
	}
	var walk func(n *ActivityNode)
	walk = func(n *ActivityNode) {
		if n == nil {
			return
		}
		if n.Info != nil {
			if n.Info.Type == 4 && claimID == 0 {
				claimID = n.Info.ID
			}
			if n.Info.Type == 12 || strings.Contains(n.Info.Payload, "QingMei") {
				if wineID == 0 {
					wineID = n.Info.ID
				}
			}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(root)
	if claimID == 0 || wineID == 0 {
		return rootID, claimID, wineID, root, fmt.Errorf("未找到青梅领种子/酿制结点")
	}
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
	mats := qingmeiMaterialItems(ctx, accountID)
	total := int64(0)
	for _, m := range mats {
		total += m.Count
	}
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
