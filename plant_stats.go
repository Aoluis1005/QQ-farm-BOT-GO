package main

// ============================================================
// 图鉴 / 分析 所需的游戏配置查询
// 覆盖：getAllPlants、getFruitPrice、getSeedPrice、getSeedLevel、
//      getItemById、getPlantByFruitId、getFruitLayerByFruitId、
//      getSeedImageBySeedId、getItemImageById、getPlantByPlantId
// ============================================================

// getAllPlants 返回所有植物条目（）。
// plantByIDMap 按 plant.id 存储，遍历即可。
func getAllPlants() []plantEntry {
	out := make([]plantEntry, 0, len(plantByIDMap))
	for _, p := range plantByIDMap {
		out = append(out, p)
	}
	return out
}

// getFruitPrice 果实单价
func getFruitPrice(fruitID int) float64 {
	if it, ok := itemInfoMap[fruitID]; ok {
		return it.Price
	}
	return 0
}

// getSeedPrice 种子价格
func getSeedPrice(seedID int) float64 {
	if it, ok := itemInfoMap[seedID]; ok {
		return it.Price
	}
	return 0
}

// getSeedLevel 种子所需等级
func getSeedLevel(seedID int) int {
	if it, ok := itemInfoMap[seedID]; ok {
		return it.Level
	}
	return 0
}

// getItemById 按物品ID取条目
func getItemByID(itemID int) (itemInfoEntry, bool) {
	it, ok := itemInfoMap[itemID]
	return it, ok
}

// getPlantByFruitId 按果实ID取植物
func getPlantByFruitID(fruitID int) (plantEntry, bool) {
	p, ok := fruitToPlantMap[fruitID]
	return p, ok
}

// getFruitLayerByFruitId 果实层级
func getFruitLayerByFruitID(fruitID int) int {
	if it, ok := itemInfoMap[fruitID]; ok {
		return it.Layer
	}
	return 0
}

// getSeedImageBySeedId 种子图片来源
// 图鉴 buildIllustratedItem 传的是果实id，经 asset_name（如 Crop_1）命中同源图片。
func getSeedImageBySeedID(seedID int) string {
	return getSeedImageBySeedIdURL(seedID)
}

// getItemImageById 物品图片 URL
// 分析接口传 plant.seed_id，正常直查即命中。
func getItemImageByID(itemID int) string {
	return GetItemImageURL(itemID)
}

// getPlantSeedInfo 果实 → 种子（seed_id）与种子等级
func getPlantSeedInfo(fruitID int) (seedID, seedLevel int) {
	if plant, ok := getPlantByFruitID(fruitID); ok && plant.SeedID > 0 {
		seedID = plant.SeedID
		seedLevel = getSeedLevel(seedID)
	}
	return
}
