package main

// ============================================================
// 图鉴 / 分析 所需的游戏配置查询（对齐 Node core/src/config/gameConfig.js）
// 覆盖：getAllPlants、getFruitPrice、getSeedPrice、getSeedLevel、
//      getItemById、getPlantByFruitId、getFruitLayerByFruitId、
//      getSeedImageBySeedId、getItemImageById、getPlantByPlantId
// ============================================================

// getAllPlants 返回所有植物条目（对齐 Node getAllPlants → plantMap.values()）。
// plantByIDMap 按 plant.id 存储，遍历即可。
func getAllPlants() []plantEntry {
	out := make([]plantEntry, 0, len(plantByIDMap))
	for _, p := range plantByIDMap {
		out = append(out, p)
	}
	return out
}

// getFruitPrice 果实单价（对齐 Node getFruitPrice：itemInfoMap[fruitId].price）
func getFruitPrice(fruitID int) float64 {
	if it, ok := itemInfoMap[fruitID]; ok {
		return it.Price
	}
	return 0
}

// getSeedPrice 种子价格（对齐 Node getSeedPrice：seedItemMap[seedId].price）
func getSeedPrice(seedID int) float64 {
	if it, ok := itemInfoMap[seedID]; ok {
		return it.Price
	}
	return 0
}

// getSeedLevel 种子所需等级（对齐 Node getSeedLevel：seedItemMap[seedId].level）
func getSeedLevel(seedID int) int {
	if it, ok := itemInfoMap[seedID]; ok {
		return it.Level
	}
	return 0
}

// getItemById 按物品ID取条目（对齐 Node getItemById → itemInfoMap）
func getItemByID(itemID int) (itemInfoEntry, bool) {
	it, ok := itemInfoMap[itemID]
	return it, ok
}

// getPlantByFruitId 按果实ID取植物（对齐 Node getPlantByFruitId → fruitToPlant）
func getPlantByFruitID(fruitID int) (plantEntry, bool) {
	p, ok := fruitToPlantMap[fruitID]
	return p, ok
}

// getFruitLayerByFruitId 果实层级（对齐 Node getFruitLayerByFruitId：itemInfoMap[fruitId].layer）
func getFruitLayerByFruitID(fruitID int) int {
	if it, ok := itemInfoMap[fruitID]; ok {
		return it.Layer
	}
	return 0
}

// getSeedImageBySeedId 种子图片 URL（对齐 Node getSeedImageBySeedId → seedImageMap）
// Go 侧图片映射由 InitImageMap 从 seed_images_named 目录建立。
func getSeedImageBySeedID(seedID int) string {
	return GetItemImageURL(seedID)
}

// getItemImageById 物品图片 URL（对齐 Node getItemImageById → seedImageMap / itemInfo.asset_name 兜底）
// Go 侧统一走 itemImageMap；未命中返回空串（前端用占位图标兜底）。
func getItemImageByID(itemID int) string {
	return GetItemImageURL(itemID)
}

// getPlantSeedInfo 果实 → 种子（seed_id）与种子等级（对齐 Node admin-illustrated-helpers getPlantSeedInfo）
func getPlantSeedInfo(fruitID int) (seedID, seedLevel int) {
	if plant, ok := getPlantByFruitID(fruitID); ok && plant.SeedID > 0 {
		seedID = plant.SeedID
		seedLevel = getSeedLevel(seedID)
	}
	return
}
