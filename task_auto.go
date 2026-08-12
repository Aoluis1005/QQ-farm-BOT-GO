package main

import (
	"context"
	"fmt"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// runTaskAuto 自动做任务：扫描可领取任务并逐个领取（对齐 Node core/src/services/task.js checkAndClaimTasks）
// 受 cfg.Automation.Task 控制，由 automationLoop 串行调度（绝不与其他游戏操作并发）。
// 可领取判定对齐 Node analyzeTaskList：IsUnlocked && !IsClaimed && total>0 && progress>=total。
func runTaskAuto(accountID string, c *gw.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	body, err := rpcRequest(ctx, accountID, taskSvc, "TaskInfo", []byte{}, 20*time.Second)
	if err != nil {
		return
	}
	taskInfo := subFieldBytes(body, 1)
	if len(taskInfo) == 0 {
		return
	}
	fs := readActFields(taskInfo)
	// 可领取收集：daily=2、growth=1、main(tasks)=3（对齐 Node buildDailyTasksForDebug / buildGrowthTasks / task_info.tasks）
	groups := [][]taskItem{
		parseTaskList(fs, 2), // daily
		parseTaskList(fs, 1), // growth
		parseTaskList(fs, 3), // main
	}
	var claimNum int
	for _, tasks := range groups {
		for _, t := range tasks {
			if !t.IsUnlocked || t.IsClaimed || t.Total <= 0 || t.Progress < t.Total {
				continue
			}
			if claimTaskRewardGo(ctx, accountID, c, t.ID) {
				claimNum++
			}
			time.Sleep(300 * time.Millisecond) // 对齐 Node doClaim 内 sleep(300)
		}
	}
	if claimNum > 0 {
		appendOpLog(accountID, "task", fmt.Sprintf("自动领取 %d 个任务奖励", claimNum))
	}
}

// claimTaskRewardGo 领取单个任务奖励，返回是否成功（对齐 Node claimTaskReward(taskId, doShare=false)）
func claimTaskRewardGo(ctx context.Context, accountID string, c *gw.Client, taskID int64) bool {
	b := proto.NewBuilder()
	b.FieldInt64(1, taskID)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := rpcRequest(cctx, accountID, taskSvc, "ClaimTaskReward", b.Bytes(), 20*time.Second)
	return err == nil
}
