package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// ============================================================
// 游戏配置加载（对齐 Node core/src/config/gameConfig.js）
// 用于背包物品分类：fruit / seed / props / other / fertilizer
// Plant.json + ItemInfo.json 位于服务器 game-config/ 目录；本地缺失时回退启发式规则。
// ============================================================

// plantEntry Plant.json 条目（仅关心分类所需字段）
type plantEntry struct {
	Name   string `json:"name"`
	SeedID int    `json:"seed_id"`
	Fruit  *struct {
		ID int `json:"id"`
	} `json:"fruit"`
}

// itemInfoEntry ItemInfo.json 条目
type itemInfoEntry struct {
	ID             int    `json:"id"`
	Type           int    `json:"type"` // 5=种子物品；4=果实物品；6=果实/装扮
	Name           string `json:"name"`
	Price          float64 `json:"price"`
	Level          int    `json:"level"`
	PriceID        int    `json:"price_id"`
	InteractionType string `json:"interaction_type"`
}

// 运行期从配置文件建立的映射
var (
	seedToPlantMap  = map[int]plantEntry{} // seed_id -> 植物
	fruitToPlantMap = map[int]plantEntry{} // fruit.id -> 植物
	itemInfoMap     = map[int]itemInfoEntry{}
	seedItemSet     = map[int]bool{} // ItemInfo type==5 的种子物品 id
)

// initGameConfig 从 gameConfigDir 加载 Plant.json / ItemInfo.json。
// 成功加载后调用方可用 IsFruitItemID / IsSeedItemID / itemName 做精确分类。
func initGameConfig(gameConfigDir string) {
	loadPlantJSON(filepath.Join(gameConfigDir, "Plant.json"))
	loadItemInfoJSON(filepath.Join(gameConfigDir, "ItemInfo.json"))
	if len(seedToPlantMap) > 0 || len(seedItemSet) > 0 {
		log.Printf("[config] 已加载植物配置(%d)与物品配置(%d)", len(seedToPlantMap), len(itemInfoMap))
	} else {
		log.Printf("[config] game-config 缺失，背包分类将使用启发式回退")
	}
}

func loadPlantJSON(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var rows []plantEntry
	if err := json.Unmarshal(data, &rows); err != nil {
		log.Printf("[config] 解析 %s 失败: %v", path, err)
		return
	}
	seedToPlantMap = make(map[int]plantEntry, len(rows))
	fruitToPlantMap = make(map[int]plantEntry, len(rows))
	for _, p := range rows {
		if p.SeedID > 0 {
			if _, ok := seedToPlantMap[p.SeedID]; !ok {
				seedToPlantMap[p.SeedID] = p
			}
		}
		if p.Fruit != nil && p.Fruit.ID > 0 {
			if _, ok := fruitToPlantMap[p.Fruit.ID]; !ok {
				fruitToPlantMap[p.Fruit.ID] = p
			}
		}
	}
}

func loadItemInfoJSON(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var rows []itemInfoEntry
	if err := json.Unmarshal(data, &rows); err != nil {
		log.Printf("[config] 解析 %s 失败: %v", path, err)
		return
	}
	itemInfoMap = make(map[int]itemInfoEntry, len(rows))
	seedItemSet = map[int]bool{}
	for _, it := range rows {
		if it.ID <= 0 {
			continue
		}
		if _, ok := itemInfoMap[it.ID]; !ok {
			itemInfoMap[it.ID] = it
		}
		if it.Type == 5 {
			seedItemSet[it.ID] = true
		}
	}
}

// ---- 背包物品分类判定（对齐 Node warehouse.js getBagDetail） ----

// isFruitItemID 是否为果实物品（Plant.json fruit.id 或已知果实 ID 范围）
func isFruitItemID(id int64) bool {
	n := int(id)
	if n <= 0 {
		return false
	}
	if _, ok := fruitToPlantMap[n]; ok {
		return true
	}
	// 回退启发式：普通果实 4xxxx，变异果实 104xxxx
	if n >= 10000 && n < 50000 {
		if _, ok := fruitItemMap[int64(n)]; ok {
			return true
		}
	}
	if n >= 1040000 && n < 1050000 {
		return true
	}
	return false
}

// isSeedItemID 是否为种子物品（ItemInfo type==5 或 Plant.json seed_id）
func isSeedItemID(id int64) bool {
	n := int(id)
	if n <= 0 {
		return false
	}
	if seedItemSet[n] {
		return true
	}
	if _, ok := seedToPlantMap[n]; ok {
		return true
	}
	// 回退启发式：种子 id 范围 20001~29999
	if n >= 20001 && n <= 29999 {
		return true
	}
	return false
}

// isFertilizerItemID 是否为化肥相关物品（对齐 Node isFertilizerRelatedItemId）
func isFertilizerItemID(id int64) bool {
	n := int(id)
	if n <= 0 {
		return false
	}
	if n == 1001 || n == 1002 {
		return false
	}
	switch n {
	case 100003, 100004, 100005, 100006, 100007, 100008, 100009, 100010, 100011, 100012:
		return true
	}
	if it, ok := itemInfoMap[n]; ok {
		t := string(it.InteractionType)
		return t == "fertilizer" || t == "fertilizerpro"
	}
	return false
}

// fruitPlantName 果实植物名（"草莓" 等，不含"果实"后缀；找不到返回空）
func fruitPlantName(id int64) string {
	if fi, ok := fruitItemMap[id]; ok {
		return fi.Name
	}
	if p, ok := fruitToPlantMap[int(id)]; ok {
		return p.Name
	}
	return ""
}

// seedPlantName 种子对应植物名（"草莓" 等；找不到返回空）
// 若 ItemInfo 有 type5 的种子条目则直接用其名（如"草莓种子"）。
func seedPlantName(id int64) string {
	if it, ok := itemInfoMap[int(id)]; ok && it.Type == 5 && it.Name != "" {
		return it.Name
	}
	if p, ok := seedToPlantMap[int(id)]; ok {
		return p.Name
	}
	return ""
}

// itemDisplayName 物品展示名（未在 ItemInfo 收录时返回 "物品{id}"）
func itemDisplayName(id int64) string {
	if it, ok := itemInfoMap[int(id)]; ok {
		return it.Name
	}
	return ""
}
