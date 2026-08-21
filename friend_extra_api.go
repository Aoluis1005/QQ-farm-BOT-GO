package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/Aoluis1005/go-farm-bot/models"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

func registerFriendExtraAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/friends/fetch-dog-info", handleFetchFriendsDogInfo)
	mux.HandleFunc("/api/friend-known-gids", handleFriendKnownGids)
	mux.HandleFunc("/api/friend-known-gids/remove", handleFriendKnownGidsRemove)
	mux.HandleFunc("/api/friend-known-gids/batch-add", handleFriendKnownGidsBatchAdd)
	mux.HandleFunc("/api/friend-known-gids/batch-remove", handleFriendKnownGidsBatchRemove)
	mux.HandleFunc("/api/friend/batch-delete", handleFriendBatchDelete)
	mux.HandleFunc("/api/friend-blacklist/update", handleFriendBlacklistUpdate)
	mux.HandleFunc("/api/friend/dog/", handleFriendDogRoute)
	mux.HandleFunc("/api/dog/gifts", handleDogGifts)
	mux.HandleFunc("/api/dog/gifts/claim", handleDogGiftsClaim)
}

func randomDelay(min, max int) {
	if max <= min {
		time.Sleep(time.Duration(min) * time.Millisecond)
		return
	}
	d := min + rand.Intn(max-min)
	time.Sleep(time.Duration(d) * time.Millisecond)
}

func handleFetchFriendsDogInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}
	platform := ""
	if acc := models.GetAccountByID(accountID); acc != nil {
		platform = acc.Platform
	}
	knownGids := models.GetAccountConfig(accountID).KnownFriendGIDs
	// 复用好友列表展示缓存（同一份 GetAll 数据，避免单独再拉一次）
	allFriends, err := getAllFriendsCached(c, accountID, platform, knownGids, false)
	if err != nil {
		writeError(w, 500, "拉取好友失败: "+err.Error())
		return
	}
	dogMap, _ := readDogCache(accountID)
	cached := map[int64]bool{}
	for g := range dogMap {
		cached[g] = true
	}

	bl := readBlacklist(accountID)
	var guardDogCount, failCount, blacklistCount int64
	for _, f := range allFriends {
		if f.GID > 0 {
			if _, ok := bl[f.GID]; ok {
				blacklistCount++
			}
		}
	}

	targets := make([]*proto.GameFriend, 0)
	for _, f := range allFriends {
		if f.GID <= 0 {
			continue
		}
		if cached[f.GID] {
			if dogMap[f.GID].DogID == 90021 {
				guardDogCount++
			}
			continue
		}
		targets = append(targets, f)
	}

	for _, f := range targets {
		_, rep, eErr := enterFriendFarm(c, f.GID, 2, "")
		if eErr != nil || rep == nil {
			failCount++
			continue
		}
		cacheFriendDog(f.GID, rep)
		if rep.DogID == 90021 {
			guardDogCount++
		}
		leaveFriendFarm(c, f.GID)
		randomDelay(150, 400)
	}

	writeJSON(w, map[string]interface{}{
		"ok":             true,
		"failCount":      failCount,
		"blacklistCount": blacklistCount,
		"guardDogCount":  guardDogCount,
		"message":        fmt.Sprintf("dog info done guard=%d fail=%d", guardDogCount, failCount),
	})
}

func handleFriendDogRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	path := r.URL.Path[len("/api/friend/dog/"):]
	gid, err := strconv.ParseInt(path, 10, 64)
	if err != nil || gid <= 0 {
		writeError(w, 400, "invalid gid")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	d, ok := getFriendDog(accountID, gid)
	if !ok {
		writeJSON(w, map[string]interface{}{"ok": true, "data": nil})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": map[string]interface{}{
		"gid":     gid,
		"dogId":   d.DogID,
		"dogName": d.DogName,
	}})
}

func handleFriendKnownGidsRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		KnownFriendGids []int64 `json:"knownFriendGids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if err := models.SetKnownFriendGids(accountID, req.KnownFriendGids); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": map[string]interface{}{
		"knownFriendGids": models.GetAccountConfig(accountID).KnownFriendGIDs,
	}})
}

func handleFriendKnownGidsRemove(w http.ResponseWriter, r *http.Request) {
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
	cur := models.GetAccountConfig(accountID).KnownFriendGIDs
	next := make([]int64, 0, len(cur))
	for _, g := range cur {
		if g != gid {
			next = append(next, g)
		}
	}
	if err := models.SetKnownFriendGids(accountID, next); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "data": map[string]interface{}{
		"knownFriendGids": next,
	}})
}

func handleFriendKnownGidsBatchAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		Gids []int64 `json:"gids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	cur := models.GetAccountConfig(accountID).KnownFriendGIDs
	set := map[int64]bool{}
	for _, g := range cur {
		set[g] = true
	}
	added := 0
	for _, g := range req.Gids {
		if g > 0 && !set[g] {
			set[g] = true
			added++
		}
	}
	next := make([]int64, 0, len(set))
	for g := range set {
		next = append(next, g)
	}
	if err := models.SetKnownFriendGids(accountID, next); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "addedCount": added, "data": map[string]interface{}{
		"knownFriendGids": next,
	}})
}

func handleFriendKnownGidsBatchRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		Gids []int64 `json:"gids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	rm := map[int64]bool{}
	for _, g := range req.Gids {
		if g > 0 {
			rm[g] = true
		}
	}
	cur := models.GetAccountConfig(accountID).KnownFriendGIDs
	next := make([]int64, 0, len(cur))
	for _, g := range cur {
		if !rm[g] {
			next = append(next, g)
		}
	}
	removed := len(cur) - len(next)
	if err := models.SetKnownFriendGids(accountID, next); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "removedCount": removed, "data": map[string]interface{}{
		"knownFriendGids": next,
	}})
}

func handleFriendBatchDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		Gids     []int64 `json:"gids"`
		Password string  `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	c, cerr := clientPool.Get(accountID)
	if cerr != nil {
		writeError(w, 400, "网关未连接: "+cerr.Error())
		return
	}
	success := make([]int64, 0)
	failed := make([]map[string]interface{}, 0)
	for _, gid := range req.Gids {
		if gid <= 0 {
			continue
		}
		if derr := doDelFriend(c, gid); derr != nil {
			failed = append(failed, map[string]interface{}{"gid": gid, "error": derr.Error()})
		} else {
			success = append(success, gid)
		}
	}
	writeJSON(w, map[string]interface{}{
		"ok":           true,
		"success":      success,
		"failed":       failed,
		"successCount": len(success),
		"failedCount":  len(failed),
		"hasPassword":  req.Password != "",
	})
}

func handleFriendKnownGids(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		accountID := resolveAccountID(r.URL.Query().Get("accountId"))
		gids := models.GetAccountConfig(accountID).KnownFriendGIDs
		writeJSON(w, map[string]interface{}{"ok": true, "data": map[string]interface{}{
			"knownFriendGids": gids,
		}})
	case http.MethodPost:
		handleFriendKnownGidsRoot(w, r)
	default:
		writeError(w, 405, "method not allowed")
	}
}
