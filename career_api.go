package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/Aoluis1005/go-farm-bot/proto"
)

// careerService 生涯统计服务
const careerService = "gamepb.careerpb.CareerService"

func registerCareerAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/career", handleCareer)
}

func handleCareer(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeError(w, 400, "没有可用的账号")
		return
	}
	c, err := clientPool.Get(accountID)
	if err != nil {
		writeError(w, 400, "网关未连接: "+err.Error())
		return
	}

	// 用缩短短超时，避免与前端 axios 超时撞车。
	// 生涯数据稳定，TTL 内读缓存避免重复请求。
	body, ok := c.CareerCached(10 * time.Second)
	if !ok {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		rep, err := c.Request(ctx, careerService, "CareerInfoGet",
			proto.EncodeCareerInfoGetRequest(), 10*time.Second)
		if err != nil {
			writeError(w, 502, "获取生涯统计失败: "+err.Error())
			return
		}
		body = rep.Body
		c.StoreCareer(body)
	}

	car := proto.DecodeCareerInfoGetReply(body)

	// 首次获取生涯时把玩家头像缓存到连接，供 /api/home/profile 复用
	if car.Avatar != "" {
		c.SetAvatar(car.Avatar)
	}

	items := decorateCareerItems(car.Items)
	levelStats := decorateCareerLevelStats(car.LevelStats)

	expMax := expUpperFor(car.Level)
	expPct := expPercentFor(car.Level, car.Exp)

	writeJSON(w, map[string]interface{}{
		"ok": true,
		"data": map[string]interface{}{
			"items":       items,
			"level_stats": levelStats,
			"player": map[string]interface{}{
				"gid":        car.GID,
				"name":       car.Name,
				"avatar":     car.Avatar,
				"openid":     car.OpenID,
				"level":      car.Level,
				"exp":        car.Exp,
				"expMax":     expMax,
				"expPercent": expPct,
			},
			"meta": map[string]interface{}{
				"achieved_levels": car.AchievedLevels,
				"stats_total":     car.StatsTotal,
				"stats_count":     car.StatsCount,
			},
		},
	})
}

// decorateCareerItems 把 CareerStatItem 装饰成前端结构（fruit_id -> name/image/rarity/level）并按 count 倒序
func decorateCareerItems(raw []*proto.CareerStatItem) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(raw))
	for _, it := range raw {
		if it.FruitID <= 0 || it.Count <= 0 {
			continue
		}
		fi, ok := fruitItemMap[it.FruitID]
		name := ""
		image := ""
		rar, lvl := int64(0), int64(0)
		if ok {
			name, rar, lvl = fi.Name, fi.Rarity, fi.Level
			image = GetItemImageURL(int(it.FruitID))
		}
		if name == "" {
			name = fmt.Sprintf("果实 %d", it.FruitID)
		}
		out = append(out, map[string]interface{}{
			"id":     it.FruitID,
			"count":  it.Count,
			"name":   name,
			"image":  image,
			"level":  lvl,
			"rarity": rar,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["count"].(int64) > out[j]["count"].(int64)
	})
	return out
}

// decorateCareerLevelStats 把 CareerLevelStat 装饰成前端结构
func decorateCareerLevelStats(raw []*proto.CareerLevelStat) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(raw))
	for _, it := range raw {
		if it.FruitID <= 0 {
			continue
		}
		fi, ok := fruitItemMap[it.FruitID]
		name := ""
		image := ""
		if ok {
			name = fi.Name
			image = GetItemImageURL(int(it.FruitID))
		}
		if name == "" {
			name = fmt.Sprintf("果实 %d", it.FruitID)
		}
		out = append(out, map[string]interface{}{
			"id":    it.FruitID,
			"count": it.Count,
			"level": it.Level,
			"name":  name,
			"image": image,
		})
	}
	return out
}
