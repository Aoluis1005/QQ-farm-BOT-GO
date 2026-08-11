package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
	actSvc  = "gamepb.activitypb.ActivityService"
	seasonSvc = "gamepb.seasonpb.SeasonService"
	solarSvc  = "gamepb.solartermspb.SolarTermsService"
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
}

// ----- List：活动列表 + 时间过滤 -----

func handleActivityList(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, actSvc, "List", []byte{}, 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", err.Error())
		return
	}
	items := ParseActivityList(body)
	now := time.Now().Unix()
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "ongoing"
	}
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
		case "all":
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
	writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "now": now, "scope": scope, "items": out})
}

// ----- Group：活动分组树 + 商店 -----

func handleActivityGroup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accountID := resolveAccountID(q.Get("accountId"))
	id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
	if id == 0 {
		writeJSONMap(w, "ok", false, "error", "id required")
		return
	}
	// GetGroup 支持 uid（可空；实测空串即可返回完整分组）
	b := proto.NewBuilder()
	b.FieldInt64(1, id)
	b.FieldString(2, q.Get("uid"))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, actSvc, "GetGroup", b.Bytes(), 15*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", err.Error())
		return
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
		writeJSONMap(w, "ok", false, "error", err.Error())
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
		writeJSONMap(w, "ok", false, "error", err.Error())
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
		writeJSONMap(w, "ok", false, "error", err.Error())
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
		writeJSONMap(w, "ok", false, "error", err.Error())
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
		writeJSONMap(w, "ok", false, "error", err.Error())
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
		writeJSONMap(w, "ok", false, "error", err.Error())
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
		writeJSONMap(w, "ok", false, "error", err.Error())
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
		es := err.Error()
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
