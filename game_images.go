package main

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// itemImageMap 物品ID(itemId) → 图片URL 映射，由 InitImageMap 启动时从 seed_images_named 目录扫描建立。
var itemImageMap = map[int]string{}

// seedAssetImageMap 资产名(assetName，如 "Crop_1") → 图片URL。
// 从文件名 "xxx_Crop_1_Seed.png" 提取资产名，供 ItemInfo.asset_name 回退匹配。
var seedAssetImageMap = map[string]string{}

// imageNamedRe 匹配 {id}_xxx.png 格式（如 20001_草莓_Crop_1_Seed.png）
var imageNamedRe = regexp.MustCompile(`^(\d+)_.*\.(png|jpg|jpeg|webp|gif)$`)

// imageNumericRe 匹配纯 {id}.png 格式（如 1001.png）
var imageNumericRe = regexp.MustCompile(`^(\d+)\.(png|jpg|jpeg|webp|gif)$`)

// imageAssetRe 匹配 ..._Crop_(\d+)_Seed.png，捕获 "Crop_X"
var imageAssetRe = regexp.MustCompile(`(Crop_\d+)_Seed\.(?:png|jpg|jpeg|webp|gif)$`)

// InitImageMap 扫描 gameConfigDir/seed_images_named 目录：
//   - 按文件名前缀数字建立 itemId → URL
//   - 按文件名中的 Crop_X_Seed 建立 assetName → URL
//   - 另外递归扫描 mutant/ 子目录（变异作物图片）
func InitImageMap(gameConfigDir string) {
	dir := filepath.Join(gameConfigDir, "seed_images_named")
	itemImageMap = make(map[int]string)
	seedAssetImageMap = make(map[string]string)
	scanImageDir(dir, "")
	// mutant 子目录（变异作物素材，路径 /mutant/*）
	scanImageDir(filepath.Join(dir, "mutant"), "mutant/")
	log.Printf("[images] 已加载物品图片映射 (id=%d, assetName=%d 项)",
		len(itemImageMap), len(seedAssetImageMap))
}

func scanImageDir(dir, urlPrefix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		sub := filepath.Join(dir, e.Name())
		if e.IsDir() {
			// 递归子目录（如 mutant 下可能再分组）
			scanImageDir(sub, urlPrefix+e.Name()+"/")
			continue
		}
		filename := e.Name()
		imageURL := "/game-config/seed_images_named/" + urlPrefix + filename

		// {id}_xxx.png 格式，优先
		if m := imageNamedRe.FindStringSubmatch(filename); m != nil {
			if id := parsePositiveInt(m[1]); id > 0 {
				if _, exists := itemImageMap[id]; !exists {
					itemImageMap[id] = imageURL
				}
			}
		}
		// 纯 {id}.png 格式
		if m := imageNumericRe.FindStringSubmatch(filename); m != nil {
			if id := parsePositiveInt(m[1]); id > 0 {
				if _, exists := itemImageMap[id]; !exists {
					itemImageMap[id] = imageURL
				}
			}
		}
		// 资产名 Crop_X_Seed → "Crop_X"
		if m := imageAssetRe.FindStringSubmatch(filename); m != nil {
			assetName := m[1]
			if _, exists := seedAssetImageMap[assetName]; !exists {
				seedAssetImageMap[assetName] = imageURL
			}
		}
	}
}

// tryGetImage 先按 id 查 itemImageMap，未命中再从 ItemInfo.asset_name → seedAssetImageMap 回退。
func tryGetImage(id int) string {
	if u, ok := itemImageMap[id]; ok && u != "" {
		return u
	}
	if it, ok := itemInfoMap[id]; ok && it.AssetName != "" {
		if u, ok2 := seedAssetImageMap[it.AssetName]; ok2 {
			return u
		}
	}
	return ""
}

// GetItemImageURL 返回 itemId 对应图片URL，
//   1. 直接查 id / asset_name
//   2. itemId 为果实 id 时，用 plant.seed_id 换算后再查
func GetItemImageURL(itemID int) string {
	if u := tryGetImage(itemID); u != "" {
		return u
	}
	if plant, ok := getPlantByFruitID(itemID); ok && plant.SeedID > 0 {
		if u := tryGetImage(plant.SeedID); u != "" {
			return u
		}
	}
	return ""
}

// getSeedImageBySeedIdURL id 直查 → asset_name 回退（不换算 fruit→seed）。
// 供图鉴 buildIllustratedItem 使用（传 fruitId，经 asset_name 命中同源图片）。
func getSeedImageBySeedIdURL(seedID int) string {
	return tryGetImage(seedID)
}

func parsePositiveInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
