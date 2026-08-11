package main

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/models"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// 神秘商人自动购买（对齐 Node mystery-scheduler.js）
// Node：启动即执行一次，之后每 60 分钟一轮；每轮逐级查询活跃 NPC 直到 inactive/同一NPC重复/货币不在允许列表
//（MYSTERY_AUTO_BUY_MAX_PER_CYCLE=20）。
// GO：遍历所有「开启该功能且已勾选货币」的在线账号顺序执行；购买之间加 500ms 间隔（对齐死规矩：
// 游戏商店操作禁止并发的风控约束）。

const (
	mysteryAutoBuyIntervalMs  = 60 * time.Minute
	mysteryAutoBuyMaxPerCycle = 20
	mysteryAutoBuyBuyTimeout  = 15 * time.Second
	mysteryAutoBuyStepDelay   = 500 * time.Millisecond
	mysteryAutoBuyStartWait   = 3 * time.Second // 启动等待初始连接建立
)

var mysteryAutoBuyService = "gamepb.mysteryshoppb.MysteryShopService"

// startMysteryAutoBuyLoop 启动后台循环：启动延迟片刻后立即执行一轮，之后每 60 分钟一轮。
func startMysteryAutoBuyLoop(ctx context.Context) {
	go func() {
		select {
		case <-time.After(mysteryAutoBuyStartWait):
		case <-ctx.Done():
			return
		}
		runMysteryAutoBuyRound(ctx)
		t := time.NewTicker(mysteryAutoBuyIntervalMs)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				runMysteryAutoBuyRound(ctx)
			}
		}
	}()
	log.Printf("[mystery] 神秘商人自动购买后台循环已启动，间隔 60 分钟")
}

// runMysteryAutoBuyRound 单轮：顺序处理所有开启自动购买的在线账号。
func runMysteryAutoBuyRound(ctx context.Context) {
	for _, acc := range models.GetAccounts() {
		cfg := models.GetAccountConfig(acc.ID)
		if !cfg.Automation.MysteryAutoBuy || len(cfg.MysteryAutoBuyCurrencies) == 0 {
			continue
		}
		c := clientPool.cached(acc.ID)
		if c == nil || c.IsClosed() {
			continue
		}
		allowed := make(map[int64]bool, len(cfg.MysteryAutoBuyCurrencies))
		for _, cur := range cfg.MysteryAutoBuyCurrencies {
			allowed[int64(cur)] = true
		}
		mysteryAutoBuyForClient(ctx, acc.ID, c, allowed)
	}
}

// mysteryAutoBuyForClient 单账号：循环查询活跃 NPC 并购买，最多 20 次。
func mysteryAutoBuyForClient(ctx context.Context, accountID string, c *gw.Client, allowed map[int64]bool) {
	lastNpcID := int64(-1)
	for i := 0; i < mysteryAutoBuyMaxPerCycle; i++ {
		rep, err := c.Request(ctx, mysteryAutoBuyService, "GetActiveNPC",
			proto.EncodeGetActiveNPCRequest(), mysteryRequestTO)
		if err != nil {
			log.Printf("[mystery] 自动购买检测失败(账号 %s): %v", accountID, err)
			return
		}
		reply := proto.DecodeGetActiveNPCReply(rep.Body)
		if reply == nil || reply.NPC == nil {
			return
		}
		if !mysteryOfferActive(reply) {
			return
		}
		npc := reply.NPC
		if npc.NpcID == lastNpcID {
			return // 同一 NPC 兜底，防死循环
		}
		if !allowed[npc.CurrencyID] {
			return // 货币不在允许列表，跳过本账号
		}
		_, err = c.Request(ctx, mysteryAutoBuyService, "Buy",
			proto.EncodeMysteryBuyRequest(npc.NpcID), mysteryAutoBuyBuyTimeout)
		if err != nil {
			log.Printf("[mystery] 自动购买失败(账号 %s npc=%d): %v", accountID, npc.NpcID, err)
			return
		}
		log.Printf("[mystery] 自动购买成功(账号 %s): %s ×%d（%s）",
			accountID, mysteryItemName(npc.ItemID), npc.ItemCount, mysteryCurrencyName(npc.CurrencyID))
		lastNpcID = npc.NpcID
		select {
		case <-ctx.Done():
			return
		case <-time.After(mysteryAutoBuyStepDelay):
		}
	}
}

// mysteryOfferActive 判断当前神秘商人是可购买的活跃状态（对齐 normalizeNPC：active + 未购买 + 未过期）。
func mysteryOfferActive(reply *proto.GetActiveNPCReply) bool {
	if reply == nil || reply.NPC == nil {
		return false
	}
	endTime := reply.EndTime
	return reply.Active && !reply.NPC.Purchased && (endTime == 0 || endTime*1000 > time.Now().UnixMilli())
}

func mysteryItemName(itemID int64) string {
	if it, ok := getItemByID(int(itemID)); ok && it.Name != "" {
		return it.Name
	}
	return "物品" + strconv.FormatInt(itemID, 10)
}

func mysteryCurrencyName(currencyID int64) string {
	if n, ok := mysteryCurrencyNames[currencyID]; ok {
		return n
	}
	return "货币" + strconv.FormatInt(currencyID, 10)
}
