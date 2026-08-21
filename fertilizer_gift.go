package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ============================================================
// 自动填充化肥
// 由 core/worker.js 的 fertilizer_gift 开关触发
// 逻辑：拉背包 → 收集化肥类道具(100003~100012 或 interaction_type=fertilizer/fertilizerpro)
//   → 读容器剩余小时(普通1011/有机1012，count 为秒/3600)
//   → 对每种化肥按"单件小时"自适应调整使用量(不超过容器上限 990h)
//   → BatchUse 批量使用 → 更新容器小时 → 记日志
// 注意：Node 无"每日防重"跳过（fertilizerGiftDoneDateKey 仅用于前端状态展示）
// 靠容器满 990h 后 count 算出 <=0 自然停止，故每次巡田都会尝试。
// ============================================================

// 化肥容器上限（小时）
const fertilizerContainerLimitHours = 990

// 普通化肥每小时提供时间（按物品 ID）
var normalFertilizerItemHours = map[int]float64{
	79873: 1, // 1h
	80514: 4, // 4h
	80003: 8, // 8h
	80132: 12, // 12h
}

// 有机化肥每小时提供时间（按物品 ID）
var organicFertilizerItemHours = map[int]float64{
	80011: 1, // 1h
	80012: 4, // 4h
	80013: 8, // 8h
	80014: 12, // 12h
}

type fertilizerPayload struct {
	id    int64
	count int64
}

type fertilizerContainerHours struct {
	normal  float64
	organic float64
}

// collectFertilizerUsePayload 
// 从背包物品中收集化肥类物品的使用负载（id → 累计 count，去重合并）
func collectFertilizerUsePayload(items []proto.BagItem) []fertilizerPayload {
	seen := map[int64]int64{}
	var order []int64
	for _, it := range items {
		if it.ID <= 0 || it.Count <= 0 {
			continue
		}
		if !isFertilizerItemID(it.ID) {
			continue
		}
		if _, ok := seen[it.ID]; !ok {
			order = append(order, it.ID)
		}
		seen[it.ID] += it.Count
	}
	out := make([]fertilizerPayload, 0, len(order))
	for _, id := range order {
		out = append(out, fertilizerPayload{id: id, count: seen[id]})
	}
	return out
}

// getContainerHoursFromBagItems 
// 从背包物品读取化肥容器剩余时间（秒 → 小时）普通 1011 / 有机 1012
func getContainerHoursFromBagItems(items []proto.BagItem) fertilizerContainerHours {
	var h fertilizerContainerHours
	for _, it := range items {
		if it.ID <= 0 || it.Count <= 0 {
			continue
		}
		switch it.ID {
		case normalFertilizerID:
			h.normal = float64(it.Count) / 3600
		case organicFertilizerID:
			h.organic = float64(it.Count) / 3600
		}
	}
	return h
}

// getFertilizerItemTypeAndHours 
// 返回化肥类型（normal/organic/other）与单件提供小时
func getFertilizerItemTypeAndHours(itemID int64) (string, float64) {
	if h, ok := normalFertilizerItemHours[int(itemID)]; ok {
		return "normal", h
	}
	if h, ok := organicFertilizerItemHours[int(itemID)]; ok {
		return "organic", h
	}
	if it, ok := itemInfoMap[int(itemID)]; ok {
		switch it.InteractionType {
		case "fertilizer":
			return "normal", 1
		case "fertilizerpro":
			return "organic", 1
		}
	}
	return "other", 0
}

// runFertilizerGiftOnce 
// 自动使用背包中的化肥类道具填充容器（容器满则自然跳过）返回本次使用数量。
func runFertilizerGiftOnce(accountID string, c *gw.Client) int64 {
	ctx := context.Background()
	rep, err := c.Request(ctx, "gamepb.itempb.ItemService", "Bag",
		proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		return 0
	}
	items := proto.DecodeBagReply(rep.Body).Items
	payloads := collectFertilizerUsePayload(items)
	if len(payloads) <= 0 {
		return 0
	}

	containerHours := getContainerHoursFromBagItems(items)
	var totalUsed int64
	usedLabels := make([]string, 0)

	for _, payload := range payloads {
		itemID := payload.id
		count := payload.count
		ftype, perItemHours := getFertilizerItemTypeAndHours(itemID)

		// 自适应调整使用量（不超过容器上限 990h）
		if ftype == "normal" || ftype == "organic" {
			current := containerHours.normal
			if ftype == "organic" {
				current = containerHours.organic
			}
			if current >= fertilizerContainerLimitHours {
				continue
			}
			if perItemHours > 0 {
				maxNeeded := fertilizerContainerLimitHours - current
				maxByHours := int64(math.Floor(maxNeeded / perItemHours))
				if count > maxByHours {
					count = maxByHours
				}
				if count <= 0 {
					continue
				}
			}
		}

		label := itemDisplayName(itemID)
		if label == "" {
			label = fmt.Sprintf("物品#%d", itemID)
		}

		// BatchUse 批量使用，失败视为 0
		used := int64(0)
		req := proto.EncodeBatchUseRequest([]proto.SellItem{{ID: itemID, Count: count, UID: 0}})
		if _, err := c.Request(ctx, "gamepb.itempb.ItemService", "BatchUse", req, 12*time.Second); err != nil {
			if proto.IsFertilizerContainerFullError(err.Error()) {
				continue // 容器已满，静默跳过
			}
		} else {
			used = count
		}

		if used > 0 {
			totalUsed += used
			usedLabels = append(usedLabels, fmt.Sprintf("%sx%d", label, used))
			if perItemHours > 0 {
				if ftype == "normal" {
					containerHours.normal += float64(used) * perItemHours
				} else if ftype == "organic" {
					containerHours.organic += float64(used) * perItemHours
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if totalUsed > 0 {
		detail := ""
		if len(usedLabels) > 0 {
			detail = " [" + strings.Join(usedLabels, "，") + "]"
		}
		log.Printf("[automation] 账号 %s 自动使用化肥类道具 x%d%s", accountID, totalUsed, detail)
		appendOpLog(accountID, "farm", fmt.Sprintf("自动使用化肥类道具 x%d%s", totalUsed, detail))
	}
	return totalUsed
}
