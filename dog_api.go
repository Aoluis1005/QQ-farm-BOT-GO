package main

import (
	"context"
	"net/http"
	"time"
)

// 护主犬同气礼包
// 【2026-08-05 抓包实锤，协议已还原】
//	查询可领数：DogService.GetDogInfo（无参，body 空）
//	  → 响应 body f7 varint = 当前可领同气礼包数量
//	领取：DogService.ClaimSkillGifts（无参，body 空）
//	  → 响应 body f3 varint = 本次领取数量
//	  （响应形如 f1{f1:101351(物品ID), f2:数量}, f3:数量）
// 两请求体均为空，走 TSDK 加密 / auth_token，与其它活动调用同一 rpcRequest 机制。

const dogGiftSvc = "gamepb.dogpb.DogService"

// GET /api/dog/gifts  查询可领同气礼包数量
func handleDogGifts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, dogGiftSvc, "GetDogInfo", []byte{}, 20*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", "查询同气礼包失败: "+err.Error())
		return
	}
	fs := readActFields(body)
	claimable := actNum(fs, 7)
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID, "claimable": claimable,
	})
}

// POST /api/dog/gifts/claim  领取同气礼包
func handleDogGiftsClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, dogGiftSvc, "ClaimSkillGifts", []byte{}, 20*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", "领取同气礼包失败: "+err.Error())
		return
	}
	fs := readActFields(body)
	claimed := actNum(fs, 3)
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID, "claimed": claimed,
	})
}
