package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// friendService 好友服务名（gamepb.friendpb.FriendService）
const friendService = "gamepb.friendpb.FriendService"

// registerFriendOpsAPI 注册农场操作 + 好友互动接口（对齐 Node admin 路由）
func registerFriendOpsAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/farm/operate", handleFarmOperate)
	mux.HandleFunc("/api/land/fertilize", handleLandFertilize)
	mux.HandleFunc("/api/friend-blacklist/toggle", handleFriendBlacklistToggle)
	mux.HandleFunc("/api/friend/", handleFriendRoute)
	mux.HandleFunc("/api/friend/apply", handleFriendApply)
}

// POST /api/farm/operate  body: { opType }
// opType: harvest / full / clear ...
func handleFarmOperate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		OpType string `json:"opType"`
		LandID string `json:"landId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	if req.OpType == "" {
		writeError(w, 400, "missing opType")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	var detail string
	switch req.OpType {
	case "clear":
		ids := parseIDs(req.LandID)
		if len(ids) == 0 {
			writeError(w, 400, "缺少 landId")
			return
		}
		if err := execFarmOp(c, "RemovePlant", proto.EncodeRemovePlantRequest(ids)); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		detail = fmt.Sprintf("铲除 %d 块地", len(ids))
	case "full", "harvest":
		if err := execFarmOp(c, "Harvest", proto.EncodeHarvestRequest(nil, c.GID, true)); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		recordOperation(accountID, "harvest", 24)
		detail = "全部收获"
	default:
		writeError(w, 400, "不支持的操作: "+req.OpType)
		return
	}
	appendOpLog(accountID, req.OpType, detail)
	writeJSON(w, map[string]interface{}{"ok": true, "opType": req.OpType, "message": detail})
}

// POST /api/land/fertilize  body: { landId }  —— 地块催熟
func handleLandFertilize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		LandID string `json:"landId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	if req.LandID == "" {
		writeError(w, 400, "missing landId")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	ids := parseIDs(req.LandID)
	if len(ids) == 0 {
		writeError(w, 400, "invalid landId")
		return
	}
	if err := execFarmOp(c, "Fertilize", proto.EncodeFertilizeRequest(ids, normalFertilizerID)); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	recordOperation(accountID, "fertilize", int64(len(ids)))
	appendOpLog(accountID, "fertilize", "催熟 "+req.LandID)
	writeJSON(w, map[string]interface{}{"ok": true, "landId": req.LandID, "message": "催熟完成"})
}

// POST /api/friend-blacklist/toggle  body: { gid }   —— 拉黑/取消拉黑（本地持久化黑名单库）
func handleFriendBlacklistToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		GID string `json:"gid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	gid, err := strconv.ParseInt(req.GID, 10, 64)
	if req.GID == "" || err != nil || gid <= 0 {
		writeError(w, 400, "missing/invalid gid")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeError(w, 400, "无可用账号")
		return
	}
	// 名称：尽力从网关取，取不到用 GID 兜底
	name := fmt.Sprintf("好友%d", gid)
	if c, err := clientPool.Get(accountID); err == nil {
		if b := getFriendBasic(c, gid); b != nil && b.Name != "" {
			name = b.Name
		}
	}
	blacklisted, _ := toggleBlacklist(accountID, gid, name)
	appendOpLog(accountID, "blacklist", fmt.Sprintf("%s好友 %d (%s)", map[bool]string{true: "拉黑", false: "取消拉黑"}[blacklisted], gid, name))
	writeJSON(w, map[string]interface{}{"ok": true, "gid": gid, "blacklisted": blacklisted, "message": "已切换黑名单"})
}

// POST /api/friend/{gid}/op   body: { opType }  opType: steal/water/weed/bug/bad
// POST /api/friend/{gid}/delete
func handleFriendRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/friend/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		writeError(w, 400, "bad path: "+r.URL.Path)
		return
	}
	gid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || gid <= 0 {
		writeError(w, 400, "invalid gid: "+parts[0])
		return
	}
	act := parts[1]

	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, cerr := clientPool.Get(accountID)
	if cerr != nil {
		writeError(w, 400, "网关未连接: "+cerr.Error())
		return
	}

	switch act {
	case "delete":
		// 对齐 Node friend-api.js delFriend → FriendService/DelFriend（真实删除）
		if derr := doDelFriend(c, gid); derr != nil {
			writeError(w, 500, "删除好友失败: "+derr.Error())
			return
		}
		appendOpLog(accountID, "delFriend", fmt.Sprintf("删除好友 %d", gid))
		writeJSON(w, map[string]interface{}{"ok": true, "gid": gid, "message": "已删除好友"})
	case "op":
		var req struct {
			OpType string `json:"opType"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, 400, "bad json")
			return
		}
		if req.OpType == "" {
			writeError(w, 400, "missing opType")
			return
		}
		// 真实执行：进入→分析→RPC→离开（对齐 friend-visit.js doFriendOperation）
		res := doFriendOperation(c, accountID, gid, req.OpType)
		writeJSON(w, map[string]interface{}{
			"ok":         res.OK,
			"opType":     res.OpType,
			"gid":        res.GID,
			"count":      res.Count,
			"bugCount":   res.BugCount,
			"weedCount":  res.WeedCount,
			"message":    res.Message,
			"error":      res.Message,
			"enterError": res.EnterError,
		})
	default:
		writeError(w, 400, "unknown action: "+act)
	}
}

// doDelFriend 删除好友（网关 FriendService/DelFriend）
func doDelFriend(c *gw.Client, gid int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	_, err := c.Request(ctx, friendService, "DelFriend", proto.EncodeDelFriendRequest(gid), 12*time.Second)
	return err
}

// POST /api/friend/apply  body: { gid, openid, shareKey }
// 主动加好友（分享卡 → UserService.ReportArkClick，对齐 Node admin-friend-routes.js /api/friend/apply + invite.js sendReportArkClick）
func handleFriendApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		GID      int64  `json:"gid"`
		OpenID   string `json:"openid"`
		ShareKey string `json:"shareKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	if req.GID <= 0 || req.OpenID == "" {
		writeError(w, 400, "请提供完整的分享卡数据：gid + openid + shareKey")
		return
	}
	shareKey := strings.ToLower(strings.TrimSpace(req.ShareKey))
	if ok, _ := regexp.MatchString(`^[0-9a-f]{32}$`, shareKey); !ok {
		writeError(w, 400, "shareKey 需为32位十六进制")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	if err := c.ReportArkClick(r.Context(), req.GID, req.OpenID, shareKey); err != nil {
		writeError(w, 400, "发送好友申请失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "gid": req.GID, "method": "ReportArkClick", "message": "好友申请已发送"})
}
