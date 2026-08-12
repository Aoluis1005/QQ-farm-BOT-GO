package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Aoluis1005/go-farm-bot/proto"
)

// 每日任务（对齐 Node core/src/services/task.js + core/src/proto/taskpb.proto）
// 字段号对齐 taskpb.proto：
//
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
		// 部分环境 TaskInfo 不可用：返回空而非报错（对齐 Node catch 返回空结构）
		writeJSON(w, map[string]interface{}{
			"ok": true, "account": accountID,
			"growth": []taskItem{}, "daily": []taskItem{}, "err": err.Error(),
		})
		return
	}
	taskInfo := subFieldBytes(body, 1)
	fs := readActFields(taskInfo)
	growth := parseTaskList(fs, 1)
	daily := parseTaskList(fs, 2)
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
