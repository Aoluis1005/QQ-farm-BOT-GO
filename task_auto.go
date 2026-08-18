package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// illustratedTicketItemID 图鉴奖励结算的点券物品ID（对齐 Node getTicketBalanceFromBag 的 500）
const illustratedTicketItemID = 500

// runTaskAuto 自动做任务：扫描可领取任务并逐个领取（对齐 Node core/src/services/task.js checkAndClaimTasks）
// 受 cfg.Automation.Task 控制，由 automationLoop 串行调度（绝不与其他游戏操作并发）。
// 可领取判定对齐 Node analyzeTaskList：IsUnlocked && !IsClaimed && total>0 && progress>=total。
func runTaskAuto(accountID string, c *gw.Client) {
	// 父 ctx 需覆盖整条序列：TaskInfo + 逐个领取(每个 300ms 间隔) + 活跃奖励 + 图鉴奖励(2×背包查询)。
	// 原先 20s 会在任务较多时把后续 RPC 全部截断（Node 每个 RPC 独立超时、无整体上限）。
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	// 图鉴奖励（点券）对齐 Node checkAndClaimIllustratedRewards：仅在点券真实到账时记日志
	if ticketGain := claimIllustratedRewardsGo(ctx, accountID, c); ticketGain > 0 {
		appendOpLog(accountID, "task", fmt.Sprintf("自动领取图鉴奖励：点券+%d", ticketGain))
	}
}

// runDailyRoutinesGo 每日例行（对齐 Node worker.ts runDailyRoutines：邮件/分享/月卡/商城免费礼/会员）。
// 独立于任务开关（不依赖 cfg.Automation.Task），由 automationLoop 登录后立即执行 + 跨天检测触发。
// 每项内部有 doneDate 内存态防重（同一天只真正执行一次，后续直接跳过）。
// 每项执行后无论结果都记日志（对齐 Node log('邮箱'/'月卡'/'会员'/'商城'/'分享', ...)），日志页每天可见。
func runDailyRoutinesGo(accountID string, c *gw.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	appendOpLog(accountID, "task", "每日例行开始")
	// 商城免费礼（对齐 Node mall.js buyFreeGifts：GetMallListBySlotType(1) → is_free 商品 → Purchase）
	if n := buyFreeGiftsGo(ctx, accountID); n > 0 {
		appendOpLog(accountID, "task", fmt.Sprintf("领取商城免费礼包 x%d", n))
	} else {
		appendOpLog(accountID, "task", "商城：今日无免费商品可领")
	}
	// 每日分享礼包（对齐 Node share.js performDailyShare：CheckCanShare → ReportShare → ClaimShareReward）
	if performDailyShareGo(ctx, accountID) {
		appendOpLog(accountID, "task", "领取每日分享礼包")
	} else {
		appendOpLog(accountID, "task", "分享：今日不可分享或已领取")
	}
	// 邮件奖励（对齐 Node email.ts checkAndClaimEmails：GetEmailList(box 1+2) → BatchClaimEmail）
	if n := claimEmailsGo(ctx, accountID); n > 0 {
		appendOpLog(accountID, "task", fmt.Sprintf("领取邮箱奖励 %d 封", n))
	} else {
		appendOpLog(accountID, "task", "邮箱：今日无待领邮件奖励")
	}
	// 月卡礼包（对齐 Node monthcard.ts performDailyMonthCardGift：GetMonthCardInfos → ClaimMonthCardReward）
	if n := claimMonthCardGo(ctx, accountID); n > 0 {
		appendOpLog(accountID, "task", fmt.Sprintf("领取月卡礼包 %d 个", n))
	} else {
		appendOpLog(accountID, "task", "月卡：无月卡或今日已领")
	}
	// QQ会员每日礼包（对齐 Node qqvip.ts performDailyVipGift：RefreshVipInfo → GetQQVipRewardsStatus → ClaimQQVipRewards）
	if n := claimVipGiftGo(ctx, accountID); n > 0 {
		appendOpLog(accountID, "task", fmt.Sprintf("领取QQ会员礼包 %d 个", n))
	} else {
		appendOpLog(accountID, "task", "会员：今日无可领会员礼包")
	}
}

// 每日礼包领取状态（对齐 Node mall.js/share.js 的 doneDateKey 内存态；跨天自动重置）
var (
	dailyGiftMu      sync.Mutex
	freeGiftDoneDate string // 商城免费礼已领日期（todayKey）
	shareDoneDate    string // 分享礼包已领日期
)

// buyFreeGiftsGo 商城免费礼（对齐 Node mall.js buyFreeGifts）
// MallService.GetMallListBySlotType(slot=1) → goods_list 中 is_free 商品 → Purchase(goods_id, 1)
func buyFreeGiftsGo(ctx context.Context, accountID string) int {
	dailyGiftMu.Lock()
	done := freeGiftDoneDate == todayKey()
	dailyGiftMu.Unlock()
	if done {
		return 0
	}
	mallSvc := "gamepb.mallpb.MallService"
	// 遍历多个 slot 找免费商品：liyangpengs buyFreeGifts 用 slot=1，但 Node 精简版前端商城
	// 用 slot=0（worker.js getMallGoodsList(0)）——不同 slot 内容不同，免费礼可能只在其中某个，
	// 全部查一遍避免漏领（每日一次，代价可忽略）
	var free []proto.MallGoods
	seen := map[int64]bool{}
	for _, slot := range []int64{1, 0} {
		rep, err := rpcRequest(ctx, accountID, mallSvc, "GetMallListBySlotType", proto.EncodeGetMallListBySlotTypeRequest(slot), 15*time.Second)
		if err != nil {
			continue
		}
		for _, g := range proto.DecodeMallListBySlotTypeReply(rep).GoodsList {
			if g.IsFree && g.GoodsID > 0 && !seen[g.GoodsID] {
				seen[g.GoodsID] = true
				free = append(free, g)
			}
		}
	}
	if len(free) == 0 {
		return 0
	}
	bought := 0
	for _, g := range free {
		if _, e := rpcRequest(ctx, accountID, mallSvc, "Purchase", proto.EncodePurchaseRequest(g.GoodsID, 1), 12*time.Second); e == nil {
			bought++
		}
		time.Sleep(300 * time.Millisecond) // 对齐 Node 逐个购买间隔
	}
	dailyGiftMu.Lock()
	freeGiftDoneDate = todayKey()
	dailyGiftMu.Unlock()
	return bought
}

// performDailyShareGo 每日分享礼包（对齐 Node share.js performDailyShare）
// 1) CheckCanShare（field1=can_share）→ 2) ReportShare{shared:true} → 3) ClaimShareReward{claimed:true}
func performDailyShareGo(ctx context.Context, accountID string) bool {
	dailyGiftMu.Lock()
	done := shareDoneDate == todayKey()
	dailyGiftMu.Unlock()
	if done {
		return false
	}
	// 1) CheckCanShare
	checkBody, err := rpcRequest(ctx, accountID, shareSvc, "CheckCanShare", []byte{}, 12*time.Second)
	if err != nil {
		return false
	}
	if actNum(readActFields(checkBody), 1) == 0 { // can_share=false → 今日无可分享
		dailyGiftMu.Lock()
		shareDoneDate = todayKey()
		dailyGiftMu.Unlock()
		return false
	}
	// 2) ReportShare {shared:true, field_4=42}（对齐参考实现：ReportShareRequest{field_1:1, field_4:42}）
	//    field_4=42 是分享场景标识，只发 field_1 服务端可能不识别导致每日分享领取失败
	repB := proto.NewBuilder()
	repB.FieldBool(1, true)
	repB.FieldInt64(4, 42)
	if _, err := rpcRequest(ctx, accountID, shareSvc, "ReportShare", repB.Bytes(), 12*time.Second); err != nil {
		return false
	}
	// 3) ClaimShareReward {claimed:true}（对齐 Node share.js claimShareReward）
	clB := proto.NewBuilder()
	clB.FieldBool(1, true)
	if _, err := rpcRequest(ctx, accountID, shareSvc, "ClaimShareReward", clB.Bytes(), 12*time.Second); err != nil {
		return false
	}
	dailyGiftMu.Lock()
	shareDoneDate = todayKey()
	dailyGiftMu.Unlock()
	return true
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
// 返回本次实际到账的点券数量（0 表示没真正领到东西）。
//
// 为何用「点券余额差」而不是数 reply 里的 item 个数：
// ClaimAllRewardsV2 即使没有可领奖励，服务端仍会返回带 items/bonus_items 的响应，
// 按 item 个数判定会导致每轮任务循环（30s）都误判成"领到 1 个奖品"并写操作日志 → 日志刷屏。
// Node 的做法是领取前后各查一次背包点券余额，只有 ticketGain>0 才算成功，这里保持一致。
func claimIllustratedRewardsGo(ctx context.Context, accountID string, c *gw.Client) int {
	before := ticketBalanceFromBag(ctx, accountID)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := rpcRequest(cctx, accountID, "gamepb.illustratedpb.IllustratedService",
		"ClaimAllRewardsV2", proto.EncodeClaimAllRewardsV2Request(true), 20*time.Second); err != nil {
		return 0
	}
	gain := ticketBalanceFromBag(ctx, accountID) - before
	if gain <= 0 {
		return 0
	}
	// 对齐 Node task.js (图鉴奖励成功)：recordOperation('taskClaim', 1)
	recordOperation(accountID, "taskClaim", 1)
	return int(gain)
}

// ticketBalanceFromBag 查询背包点券余额（对齐 Node task.js getTicketBalanceFromBag：物品 ID=500）
// 注意：此处的 500 与首页资产读取用的 proto.ItemIDCoupon(1002) 不是同一个物品，勿混用。
func ticketBalanceFromBag(ctx context.Context, accountID string) int64 {
	cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	rep, err := rpcRequest(cctx, accountID, "gamepb.itempb.ItemService", "Bag",
		proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		return 0
	}
	for _, it := range proto.DecodeBagReply(rep).Items {
		if it.ID == illustratedTicketItemID {
			if it.Count < 0 {
				return 0
			}
			return it.Count
		}
	}
	return 0
}

// claimTaskRewardGo 领取单个任务奖励，返回是否成功（对齐 Node claimTaskReward(taskId, doShare=false)）
func claimTaskRewardGo(ctx context.Context, accountID string, c *gw.Client, taskID int64) bool {
	b := proto.NewBuilder()
	b.FieldInt64(1, taskID)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := rpcRequest(cctx, accountID, taskSvc, "ClaimTaskReward", b.Bytes(), 20*time.Second)
	if err == nil {
		// 对齐 Node task.js doClaim：领取成功 recordOperation('taskClaim', 1)
		recordOperation(accountID, "taskClaim", 1)
		return true
	}
	return false
}

// emailSvc / monthCardSvc / vipSvc 服务名（对齐 Node sendMsgAsync 首参）
const (
	emailSvc     = "gamepb.emailpb.EmailService"
	vipSvc       = "gamepb.qqvippb.QQVipService"
	monthCardSvc = "gamepb.mallpb.MallService"
)

// emailItemInfo 邮件条目（对齐 Node EmailItem 关键字段 + box 归属）
type emailItemInfo struct {
	Box       int64  // 所属邮箱类型（1/2）
	ID        string // field1 id
	Claimed   bool   // field4 claimed
	HasReward bool   // field5 has_reward
}

// claimEmailsGo 邮件奖励领取（对齐 Node email.ts checkAndClaimEmails）
// EmailService.GetEmailList(box 1+2) → 找 has_reward && !claimed → BatchClaimEmail（失败逐条单领）
// 返回成功领取的邮件数。
func claimEmailsGo(ctx context.Context, accountID string) int {
	dailyGiftMu.Lock()
	done := emailDoneDate == todayKey()
	dailyGiftMu.Unlock()
	if done {
		return 0
	}
	var claimable []emailItemInfo
	queried := false // 至少一个 box 查询成功才算"已检查"（避免 RPC 失败误报无可领）
	for _, box := range []int64{1, 2} {
		rep, err := rpcRequest(ctx, accountID, emailSvc, "GetEmailList",
			proto.EncodeGetEmailListRequest(box), 12*time.Second)
		if err != nil {
			continue
		}
		queried = true
		// GetEmailListReply{ emails=1(repeated EmailItem) }
		for _, f := range readActFields(rep) {
			if f.No != 1 || f.Wire != 2 {
				continue
			}
			em := emailItemInfo{Box: box}
			for _, ef := range readActFields(f.Bytes) {
				switch {
				case ef.No == 1 && ef.Wire == 2:
					em.ID = string(ef.Bytes)
				case ef.No == 4 && ef.Wire == 0:
					em.Claimed = ef.Varint != 0
				case ef.No == 5 && ef.Wire == 0:
					em.HasReward = ef.Varint != 0
				}
			}
			if em.ID != "" && !em.Claimed && em.HasReward {
				claimable = append(claimable, em)
			}
		}
	}
	if len(claimable) == 0 {
		dailyGiftMu.Lock()
		emailDoneDate = todayKey()
		dailyGiftMu.Unlock()
		if queried {
			appendOpLog(accountID, "task", "邮箱：今日无可领取邮件奖励")
		}
		return 0
	}
	// 批量领取（按 box 分组，对齐 Node byBox）
	byBox := map[int64][]string{}
	for _, em := range claimable {
		byBox[em.Box] = append(byBox[em.Box], em.ID)
	}
	batchOK := map[string]bool{} // "box:id" 已批量领取
	claimed := 0
	for box, ids := range byBox {
		if _, err := rpcRequest(ctx, accountID, emailSvc, "BatchClaimEmail",
			proto.EncodeBatchClaimEmailRequest(box, ids), 12*time.Second); err == nil {
			for _, id := range ids {
				batchOK[fmt.Sprintf("%d:%s", box, id)] = true
			}
			claimed += len(ids)
		}
		time.Sleep(300 * time.Millisecond) // 对齐 Node 批量后短暂间隔
	}
	// 批量失败的逐条单领（对齐 Node 批量失败静默 continue 单领）
	for _, em := range claimable {
		key := fmt.Sprintf("%d:%s", em.Box, em.ID)
		if batchOK[key] {
			continue
		}
		if _, err := rpcRequest(ctx, accountID, emailSvc, "ClaimEmail",
			proto.EncodeClaimEmailRequest(em.Box, em.ID), 12*time.Second); err == nil {
			claimed++
		}
		time.Sleep(300 * time.Millisecond)
	}
	dailyGiftMu.Lock()
	emailDoneDate = todayKey()
	dailyGiftMu.Unlock()
	return claimed
}

// claimMonthCardGo 月卡礼包领取（对齐 Node monthcard.ts performDailyMonthCardGift）
// MallService.GetMonthCardInfos → infos 中 can_claim && goods_id>0 → ClaimMonthCardReward(goods_id)
// 返回成功领取数。
func claimMonthCardGo(ctx context.Context, accountID string) int {
	dailyGiftMu.Lock()
	done := monthCardDoneDate == todayKey()
	dailyGiftMu.Unlock()
	if done {
		return 0
	}
	rep, err := rpcRequest(ctx, accountID, monthCardSvc, "GetMonthCardInfos",
		proto.EncodeGetMonthCardInfosRequest(), 12*time.Second)
	if err != nil {
		return 0
	}
	// GetMonthCardInfosReply{ infos=1(repeated MonthCardInfo) }
	// MonthCardInfo{ goods_id=1, reward=2, can_claim=3 }
	var goodsIDs []int64
	for _, f := range readActFields(rep) {
		if f.No != 1 || f.Wire != 2 {
			continue
		}
		canClaim := actNum(readActFields(f.Bytes), 3)
		gid := actNum(readActFields(f.Bytes), 1)
		if canClaim != 0 && gid > 0 {
			goodsIDs = append(goodsIDs, gid)
		}
	}
	if len(goodsIDs) == 0 {
		dailyGiftMu.Lock()
		monthCardDoneDate = todayKey()
		dailyGiftMu.Unlock()
		appendOpLog(accountID, "task", "月卡：当前没有月卡或今日无可领取月卡礼包")
		return 0
	}
	claimed := 0
	for _, gid := range goodsIDs {
		if _, err := rpcRequest(ctx, accountID, monthCardSvc, "ClaimMonthCardReward",
			proto.EncodeClaimMonthCardRewardRequest(gid), 12*time.Second); err == nil {
			claimed++
		}
		time.Sleep(300 * time.Millisecond)
	}
	dailyGiftMu.Lock()
	monthCardDoneDate = todayKey()
	dailyGiftMu.Unlock()
	return claimed
}

// claimVipGiftGo QQ会员每日礼包（对齐 Node qqvip.ts performDailyVipGift）
// RefreshVipInfo → GetQQVipRewardsStatus → reward_statuses 中 enabled && can_claim 的
// reward_type → ClaimQQVipRewards(reward_types)。返回成功领取的档位数。
func claimVipGiftGo(ctx context.Context, accountID string) int {
	dailyGiftMu.Lock()
	done := vipDoneDate == todayKey()
	dailyGiftMu.Unlock()
	if done {
		return 0
	}
	if _, err := rpcRequest(ctx, accountID, vipSvc, "RefreshVipInfo",
		proto.EncodeEmptyRequest(), 12*time.Second); err != nil {
		return 0
	}
	rep, err := rpcRequest(ctx, accountID, vipSvc, "GetQQVipRewardsStatus",
		proto.EncodeEmptyRequest(), 12*time.Second)
	if err != nil {
		return 0
	}
	// GetQQVipRewardsStatusReply{ reward_statuses=5(repeated QQVipRewardStatus) }
	// QQVipRewardStatus{ enabled=1, reward_type=5, can_claim=6 }
	var rewardTypes []int64
	for _, f := range readActFields(rep) {
		if f.No != 5 || f.Wire != 2 {
			continue
		}
		fs := readActFields(f.Bytes)
		enabled := actNum(fs, 1)
		canClaim := actNum(fs, 6)
		rt := actNum(fs, 5)
		if enabled != 0 && canClaim != 0 && rt > 0 {
			rewardTypes = append(rewardTypes, rt)
		}
	}
	if len(rewardTypes) == 0 {
		dailyGiftMu.Lock()
		vipDoneDate = todayKey()
		dailyGiftMu.Unlock()
		appendOpLog(accountID, "task", "会员：今日暂无可领取QQ会员礼包")
		return 0
	}
	if _, err := rpcRequest(ctx, accountID, vipSvc, "ClaimQQVipRewards",
		proto.EncodeClaimQQVipRewardsRequest(rewardTypes), 12*time.Second); err != nil {
		return 0
	}
	dailyGiftMu.Lock()
	vipDoneDate = todayKey()
	dailyGiftMu.Unlock()
	return len(rewardTypes)
}

// monthCardDoneDate / emailDoneDate / vipDoneDate 每日领取状态（对齐 Node doneDateKey 内存态；跨天自动重置）
var (
	emailDoneDate     string
	monthCardDoneDate string
	vipDoneDate       string
)
