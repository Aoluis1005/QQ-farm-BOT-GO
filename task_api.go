package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Aoluis1005/go-farm-bot/proto"
)

// 每日任务
// 字段号
//	TaskInfo: growth_tasks=1, daily_tasks=2, tasks=3, actives=4
//	Task: id=1, progress=2, is_claimed=3(bool), is_unlocked=4, total_progress=6, desc=9, task_type=10
//	TaskInfoReply: task_info=1
//	ClaimTaskRewardRequest: id=1, do_shared=2
const taskSvc = "gamepb.taskpb.TaskService"

type taskItem struct {
	ID         int64  `json:"id"`
	Desc       string `json:"desc"`
	Progress   int64  `json:"progress"`
	Total      int64  `json:"total"`
	IsClaimed  bool   `json:"is_claimed"`
	IsUnlocked bool   `json:"is_unlocked"`
	TaskType   int64  `json:"task_type"` // Task.task_type=10（1=成长, 2=每日）；用于 daily_tasks/growth_tasks 为空时回退筛选
}

// parseTaskList 从任务信息字段块中解析指定 repeated 字段(任务列表)的每个任务
func parseTaskList(taskInfoFields []actField, fieldNo int) []taskItem {
	var out []taskItem
	for _, f := range taskInfoFields {
		if f.No != fieldNo || f.Wire != 2 || len(f.Bytes) == 0 {
			continue
		}
		tf := readActFields(f.Bytes)
		out = append(out, taskItem{
			ID:         actNum(tf, 1),
			Progress:   actNum(tf, 2),
			IsClaimed:  actNum(tf, 3) != 0,
			IsUnlocked: actNum(tf, 4) != 0,
			Total:      actNum(tf, 6),
			Desc:       string(actBytes(tf, 9)),
			TaskType:   actNum(tf, 10),
		})
	}
	return out
}

// GET /api/task/daily  查询每日任务+成长任务状态
func handleTaskDaily(w http.ResponseWriter, r *http.Request) {
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
	body, err := rpcRequest(ctx, accountID, taskSvc, "TaskInfo", []byte{}, 20*time.Second)
	if err != nil {
		// 部分环境 TaskInfo 不可用：返回空而非报错
		writeJSON(w, map[string]interface{}{
			"ok": true, "account": accountID,
			"growth": []taskItem{}, "daily": []taskItem{}, "err": err.Error(),
		})
		return
	}
	taskInfo := subFieldBytes(body, 1)
	fs := readActFields(taskInfo)
	growthRaw := parseTaskList(fs, 1) // growth_tasks
	dailyRaw := parseTaskList(fs, 2)  // daily_tasks
	mainRaw := parseTaskList(fs, 3)   // tasks

	// 部分服务器不单独下发 daily_tasks/growth_tasks，而是把每日/成长任务混在 tasks(field3)
	// 里用 task_type 标识（每日=2, 成长=1）。此时 daily_tasks/growth_tasks 为空，需按 task_type 回退筛选，
	// 否则每日任务页永远拿到空列表（用户反馈"每日任务无数据"的后端根因）。
	daily := dailyRaw
	if len(daily) == 0 {
		for _, t := range mainRaw {
			if t.TaskType == 2 {
				daily = append(daily, t)
			}
		}
		for _, t := range growthRaw {
			if t.TaskType == 2 {
				daily = append(daily, t)
			}
		}
	}
	growth := growthRaw
	if len(growth) == 0 {
		for _, t := range mainRaw {
			if t.TaskType == 1 {
				growth = append(growth, t)
			}
		}
	}
	// 完成数（每日任务：进度>=总进度 计为完成；成长同理）
	dailyDone, dailyTotal := countTaskDone(daily)
	growthDone, growthTotal := countTaskDone(growth)
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"growth":           growth,
		"daily":            daily,
		"daily_done":       dailyDone,
		"daily_total":      dailyTotal,
		"growth_done":      growthDone,
		"growth_total":     growthTotal,
		"daily_claimable":  countClaimable(daily),
		"growth_claimable": countClaimable(growth),
		// 诊断：服务端原始下发结构（便于判断任务是走 daily_tasks 还是混在 tasks 里用 task_type 标识）
		"src": map[string]int{
			"daily_tasks": len(dailyRaw), "growth_tasks": len(growthRaw), "tasks": len(mainRaw),
		},
	})
}

func countTaskDone(tasks []taskItem) (int, int) {
	done, total := 0, 0
	for _, t := range tasks {
		if t.Total > 0 {
			total++
			if t.Progress >= t.Total {
				done++
			}
		}
	}
	return done, total
}

func countClaimable(tasks []taskItem) int {
	n := 0
	for _, t := range tasks {
		if t.IsUnlocked && !t.IsClaimed && t.Total > 0 && t.Progress >= t.Total {
			n++
		}
	}
	return n
}

// POST /api/task/claim  body: { taskId, shared? }  领取单个任务奖励
func handleTaskClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	var req struct {
		TaskID int64 `json:"taskId"`
		Shared bool  `json:"shared"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.TaskID <= 0 {
		writeJSONMap(w, "ok", false, "error", "缺少 taskId")
		return
	}
	b := proto.NewBuilder()
	b.FieldInt64(1, req.TaskID)
	if req.Shared {
		b.FieldBool(2, true)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	_, err := rpcRequest(ctx, accountID, taskSvc, "ClaimTaskReward", b.Bytes(), 20*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", "领取任务奖励失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "claimed_task": req.TaskID})
}

// GET /api/task/daily-gifts 每日礼包领取状态（商城免费礼/分享/邮件/月卡/会员）
// POST /api/task/daily-gifts/claim body: { type: "mall"|"share"|"email"|"monthcard"|"vip"|"all" }
//   手动触发对应每日礼包领取
func handleTaskDailyGifts(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	dailyGiftMu.Lock()
	state := map[string]interface{}{
		"mall":      freeGiftDoneDate[accountID] == todayKey(),
		"share":     shareDoneDate[accountID] == todayKey(),
		"email":     emailDoneDate[accountID] == todayKey(),
		"monthcard": monthCardDoneDate[accountID] == todayKey(),
		"vip":       vipDoneDate[accountID] == todayKey(),
	}
	dailyGiftMu.Unlock()

	if r.Method != http.MethodPost {
		writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "state": state})
		return
	}
	var req struct {
		Type string `json:"type"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	result := map[string]int{}
	switch req.Type {
	case "mall":
		result["mall"] = buyFreeGiftsGo(ctx, accountID)
	case "share":
		if performDailyShareGo(ctx, accountID) {
			result["share"] = 1
		}
	case "email":
		result["email"] = claimEmailsGo(ctx, accountID)
	case "monthcard":
		result["monthcard"] = claimMonthCardGo(ctx, accountID)
	case "vip":
		result["vip"] = claimVipGiftGo(ctx, accountID)
	default: // all 或缺省：全部执行
		result["mall"] = buyFreeGiftsGo(ctx, accountID)
		if performDailyShareGo(ctx, accountID) {
			result["share"] = 1
		}
		result["email"] = claimEmailsGo(ctx, accountID)
		result["monthcard"] = claimMonthCardGo(ctx, accountID)
		result["vip"] = claimVipGiftGo(ctx, accountID)
	}
	writeJSON(w, map[string]interface{}{"ok": true, "account": accountID, "result": result})
}

// registerTaskAPI 注册每日/成长任务相关接口（b6a4961 仅加了 handler 却漏注册，导致 /api/task/daily 一直 404 → 每日任务页长期无数据）
func registerTaskAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/task/daily", handleTaskDaily)
	mux.HandleFunc("/api/task/claim", handleTaskClaim)
	mux.HandleFunc("/api/task/daily-gifts", handleTaskDailyGifts)
}
