package main

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// itemImageMap 物品ID(itemId) → 图片URL 映射，由 InitImageMap 启动时从 seed_images_named 目录扫描建立。
// 对齐 Node 端 gameConfig.js 的 seedImageMap 逻辑。
var itemImageMap = map[int]string{}

// imageNamedRe 匹配 {id}_xxx.png 格式（如 1001_carrot.png）
var imageNamedRe = regexp.MustCompile(`^(\d+)_.*\.(png|jpg|jpeg|webp|gif)$`)

// imageNumericRe 匹配纯 {id}.png 格式（如 1001.png）
var imageNumericRe = regexp.MustCompile(`^(\d+)\.(png|jpg|jpeg|webp|gif)$`)

// InitImageMap 扫描 gameConfigDir/seed_images_named 目录，把 itemId → 图片URL 写入 itemImageMap。
// 服务 WorkingDirectory 为游戏配置根目录（如 /home/ubuntu/go-farm-bot），直接传 "game-config"。
func InitImageMap(gameConfigDir string) {
	dir := filepath.Join(gameConfigDir, "seed_images_named")
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("[images] 扫描 seed_images_named 失败: %v", err)
		return
	}
	itemImageMap = make(map[int]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		filename := e.Name()
		imageURL := "/game-config/seed_images_named/" + filename

		// 匹配 {id}_xxx.png 格式，优先（首个命中者保留，后出现同 id 不覆盖，对齐 Node 行为）
		if m := imageNamedRe.FindStringSubmatch(filename); m != nil {
			if id := parsePositiveInt(m[1]); id > 0 {
				if _, exists := itemImageMap[id]; !exists {
					itemImageMap[id] = imageURL
				}
				continue
			}
		}

		// 匹配纯 {id}.png 格式
		if m := imageNumericRe.FindStringSubmatch(filename); m != nil {
			if id := parsePositiveInt(m[1]); id > 0 {
				if _, exists := itemImageMap[id]; !exists {
					itemImageMap[id] = imageURL
				}
			}
		}
	}
	log.Printf("[images] 已加载物品图片映射 (%d 项)", len(itemImageMap))
}

// GetItemImageURL 返回 itemId 对应图片 URL；未命中返回空字符串。
func GetItemImageURL(itemId int) string {
	return itemImageMap[itemId]
}

func parsePositiveInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
