package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
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
	api.HandleFunc("/api/activity/solar", handleActivitySolar)
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
	for _, it := range items {
		longTerm := it.StartTime <= 0
		ongoing := longTerm || (it.StartTime <= now && it.EndTime >= now)
		upcoming := !longTerm && it.StartTime > now
		finished := !longTerm && it.EndTime > 0 && it.EndTime < now
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
