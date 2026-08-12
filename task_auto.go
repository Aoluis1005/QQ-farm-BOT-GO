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

	// 活跃奖励（日/周活跃度档位）对齐 Node checkAndClaimActives
	if activeClaimed := claimActivesGo(ctx, accountID, c, fs); activeClaimed > 0 {
		appendOpLog(accountID, "task", fmt.Sprintf("自动领取 %d 个活跃奖励", activeClaimed))
	}
	// 图鉴奖励（点券）对齐 Node checkAndClaimIllustratedRewards
	if illCnt := claimIllustratedRewardsGo(ctx, accountID, c); illCnt > 0 {
		appendOpLog(accountID, "task", fmt.Sprintf("自动领取图鉴奖励 %d 个奖品", illCnt))
	}
}

// claimActivesGo 领取日/周活跃奖励（对齐 Node task.js checkAndClaimActives）
// task_info.actives=4 → Active{type=1, progress=2, rewards=3}
// ActiveReward{point_id=1, need_progress=2, status=3}，ActiveStatus.DONE=2
func claimActivesGo(ctx context.Context, accountID string, c *gw.Client, taskInfoFields []actField) int {
	var claimed int
	for _, f := range taskInfoFields {
		if f.No != 4 || f.Wire != 2 || len(f.Bytes) == 0 {
			continue // actives 不在 task_info 顶层，跳过
		}
		afs := readActFields(f.Bytes)
		typ := int(actNum(afs, 1)) // Active.type (1=日, 2=周)
		var pointIDs []int64
		for _, rf := range afs {
			if rf.No != 3 || rf.Wire != 2 || len(rf.Bytes) == 0 {
				continue // active.rewards=3
			}
			rewardFields := readActFields(rf.Bytes)
			if actNum(rewardFields, 3) != 2 { // status != ActiveStatus.DONE
				continue
			}
			if pid := actNum(rewardFields, 1); pid > 0 { // point_id=1
				pointIDs = append(pointIDs, pid)
			}
		}
		if len(pointIDs) == 0 {
			continue
		}
		if claimDailyRewardGo(ctx, accountID, c, typ, pointIDs) {
			claimed += len(pointIDs)
		}
		time.Sleep(300 * time.Millisecond)
	}
	return claimed
}

// claimDailyRewardGo 领取活跃度档位奖励（ClaimDailyRewardRequest: type=1, point_ids=2）
func claimDailyRewardGo(ctx context.Context, accountID string, c *gw.Client, typ int, pointIDs []int64) bool {
	b := proto.NewBuilder()
	b.FieldInt64(1, int64(typ))
	for _, pid := range pointIDs {
		b.FieldInt64(2, pid)
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := rpcRequest(cctx, accountID, taskSvc, "ClaimDailyReward", b.Bytes(), 20*time.Second)
	return err == nil
}

// claimIllustratedRewardsGo 领取全部已达标图鉴奖励（对齐 Node checkAndClaimIllustratedRewards）
func claimIllustratedRewardsGo(ctx context.Context, accountID string, c *gw.Client) int {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rep, err := rpcRequest(cctx, accountID, "gamepb.illustratedpb.IllustratedService",
		"ClaimAllRewardsV2", proto.EncodeClaimAllRewardsV2Request(true), 20*time.Second)
	if err != nil {
		return 0
	}
	return proto.DecodeClaimAllRewardsV2Reply(rep)
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
