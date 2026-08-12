package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/models"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ============================================================
// 好友服务层：访问/离开好友农场、地块分析、好友操作、狗信息缓存、黑名单本地库。
// 协议对齐 Node core/src/services/friend-api.js / friend-visit.js / friend-operation-limits.js。
// ============================================================

const visitService = "gamepb.visitpb.VisitService"

// friendVisitTimeout 单次访问好友农场超时
const friendVisitTimeout = 15 * time.Second

// enterFriendFarm 进入好友农场（reason 默认 2=偷菜访问；visitToken 用于主动加好友 32hex nonce）。
// 返回原始响应字节（供取 nonce）与解析结果。
func enterFriendFarm(c *gw.Client, gid int64, reason int64, visitToken string) (raw []byte, rep *proto.VisitEnterReply, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), friendVisitTimeout)
	defer cancel()
	msg, err := c.Request(ctx, visitService, "Enter",
		proto.EncodeVisitEnterRequest(gid, reason, visitToken), friendVisitTimeout)
	if err != nil {
		return nil, nil, err
	}
	rep = proto.DecodeVisitEnterReply(msg.Body)
	return msg.Body, rep, nil
}

func leaveFriendFarm(c *gw.Client, gid int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = c.Request(ctx, visitService, "Leave", proto.EncodeVisitLeaveRequest(gid), 10*time.Second)
}

// friendLandsAnalysis 好友地块分类结果（对齐 Node friend-land-analyzer.js analyzeFriendLands）
type friendLandsAnalysis struct {
	Stealable  []int64
	NeedWater  []int64
	NeedWeed   []int64
	NeedBug    []int64
	CanPutWeed []int64
	CanPutBug  []int64
}

// analyzeFriendLands 分析好友所有地块，产出可操作分类。
func analyzeFriendLands(lands []*proto.LandInfo, myGid int64) *friendLandsAnalysis {
	out := &friendLandsAnalysis{}
	now := time.Now().Unix()
	for _, land := range lands {
		p := land.Plant
		if p == nil || len(p.Phases) == 0 {
			continue
		}
		current := currentPhase(p.Phases, now)
		if current == nil {
			continue
		}
		phase := current.Phase

		// 成熟 & 可偷
		if phase == proto.PhaseMature {
			if p.Stealable {
				out.Stealable = append(out.Stealable, land.ID)
			}
			continue
		}
		// 枯死跳过
		if phase == proto.PhaseDead {
			continue
		}

		// 缺水/草/虫（用 num 判定，与 Node 一致）
		if p.DryNum > 0 {
			out.NeedWater = append(out.NeedWater, land.ID)
		}
		if p.WeedNum > 0 || len(p.WeedOwners) > 0 {
			out.NeedWeed = append(out.NeedWeed, land.ID)
		}
		if p.InsectNum > 0 || len(p.InsectOwners) > 0 {
			out.NeedBug = append(out.NeedBug, land.ID)
		}

		// 可放草/放虫（同主人上限 2，且自己未放过）
		weedOwners := p.WeedOwners
		bugOwners := p.InsectOwners
		alreadyWeed := containsInt64(weedOwners, myGid)
		alreadyBug := containsInt64(bugOwners, myGid)
		if len(weedOwners) < 2 && !alreadyWeed {
			out.CanPutWeed = append(out.CanPutWeed, land.ID)
		}
		if len(bugOwners) < 2 && !alreadyBug {
			out.CanPutBug = append(out.CanPutBug, land.ID)
		}
	}
	return out
}

func containsInt64(a []int64, v int64) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}

// currentPhase 取当前生长阶段（begin_time<=now 的最大值），对齐 Node getCurrentPhase(…, false)
func currentPhase(phases []*proto.PlantPhaseInfo, now int64) *proto.PlantPhaseInfo {
	var cur *proto.PlantPhaseInfo
	for _, ph := range phases {
		if ph.BeginTime > 0 && ph.BeginTime <= now {
			cur = ph
		}
	}
	if cur == nil && len(phases) > 0 {
		return phases[0]
	}
	return cur
}

// doFriendOperationResult 好友操作结果（对齐 Node doFriendOperation 返回结构）
type doFriendOperationResult struct {
	OK        bool   `json:"ok"`
	OpType    string `json:"opType"`
	GID       int64  `json:"gid"`
	Count     int64  `json:"count"`
	BugCount  int64  `json:"bugCount"`
	WeedCount int64  `json:"weedCount"`
	Message   string `json:"message"`
	// 进入失败时的特殊标记
	EnterError string `json:"enterError,omitempty"`
	DogID      int64  `json:"-"`
	DogName    string `json:"-"`
}

// doFriendOperation 对好友执行单个操作（steal/water/weed/bug/bad），完整走 进入→操作→离开。
func doFriendOperation(c *gw.Client, accountID string, gid int64, opType string) *doFriendOperationResult {
	if gid <= 0 {
		return &doFriendOperationResult{OK: false, OpType: opType, GID: gid, Message: "无效好友ID"}
	}

	// 1. 进入好友农场
	_, enterReply, err := enterFriendFarm(c, gid, 2, "")
	if err != nil {
		// 分类处理进入失败（对齐 Node friend-api.js handleFriendEnterError）：
		// 1002003 封禁→自动加黑名单；1002002/关键词→无效好友自动移出已知列表
		handleFriendEnterError(c, accountID, gid, err)
		return &doFriendOperationResult{OK: false, OpType: opType, GID: gid,
			Message: "进入好友农场失败: " + err.Error(), EnterError: err.Error()}
	}
	defer leaveFriendFarm(c, gid)

	// 顺手缓存狗信息（供好友卡片"护主犬"徽标）
	cacheFriendDog(gid, enterReply)

	lands := enterReply.Lands
	if len(lands) == 0 {
		return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, Message: "对方没有种植地块"}
	}

	analysis := analyzeFriendLands(lands, c.GID)
	var okCount int64

	switch opType {
	case "steal":
		if len(analysis.Stealable) == 0 {
			return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, Message: "没有可偷取土地"}
		}
		if err := execFriendOp(c, "Harvest", proto.EncodeHarvestRequest(analysis.Stealable, gid, true)); err != nil {
			return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, Message: "Ta已经被偷的精光了QAQ"}
		}
		okCount = int64(len(analysis.Stealable))
		if okCount > 0 {
			recordOperation(accountID, "steal", okCount)
		}
		return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: okCount, Message: fmt.Sprintf("偷取完成 %d 块", okCount)}

	case "water":
		if len(analysis.NeedWater) == 0 {
			return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, Message: "没有可浇水土地"}
		}
		if err := execFriendOp(c, "WaterLand", proto.EncodeWaterLandRequest(analysis.NeedWater, gid)); err != nil {
			return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, Message: "浇水失败，来晚一步，可惜"}
		}
		okCount = int64(len(analysis.NeedWater))
		recordOperation(accountID, "helpWater", okCount)
		return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: okCount, Message: fmt.Sprintf("浇水完成 %d 块", okCount)}

	case "weed":
		if len(analysis.NeedWeed) == 0 {
			return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, Message: "没有可除草土地"}
		}
		if err := execFriendOp(c, "WeedOut", proto.EncodeWeedOutRequest(analysis.NeedWeed, gid)); err != nil {
			return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, Message: "除草失败，来晚一步，可惜"}
		}
		okCount = int64(len(analysis.NeedWeed))
		recordOperation(accountID, "helpWeed", okCount)
		return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: okCount, Message: fmt.Sprintf("除草完成 %d 块", okCount)}

	case "bug":
		if len(analysis.NeedBug) == 0 {
			return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, Message: "没有可除虫土地"}
		}
		if err := execFriendOp(c, "Insecticide", proto.EncodeInsecticideRequest(analysis.NeedBug, gid)); err != nil {
			return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, Message: "除虫失败，来晚一步，可惜"}
		}
		okCount = int64(len(analysis.NeedBug))
		recordOperation(accountID, "helpBug", okCount)
		return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: okCount, Message: fmt.Sprintf("除虫完成 %d 块", okCount)}

	case "bad":
		var bugCount, weedCount int64
		failed := ""
		if len(analysis.CanPutBug) == 0 && len(analysis.CanPutWeed) == 0 {
			return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, BugCount: 0, WeedCount: 0, Message: "没有可捣乱土地"}
		}
		if len(analysis.CanPutBug) > 0 {
			if err := execFriendOp(c, "PutInsects", proto.EncodePutInsectsRequest(gid, analysis.CanPutBug)); err != nil {
				failed = "放虫失败"
			} else {
				bugCount = int64(len(analysis.CanPutBug))
				recordOperation(accountID, "bug", bugCount)
			}
		}
		if len(analysis.CanPutWeed) > 0 {
			if err := execFriendOp(c, "PutWeeds", proto.EncodePutWeedsRequest(gid, analysis.CanPutWeed)); err != nil {
				if failed == "" {
					failed = "放草失败"
				} else {
					failed += "/放草失败"
				}
			} else {
				weedCount = int64(len(analysis.CanPutWeed))
				recordOperation(accountID, "weed", weedCount)
			}
		}
		total := bugCount + weedCount
		if total <= 0 {
			return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, BugCount: bugCount, WeedCount: weedCount,
				Message: "捣乱失败或今日次数已用完"}
		}
		return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: total, BugCount: bugCount, WeedCount: weedCount,
			Message: fmt.Sprintf("捣乱完成 虫%d/草%d", bugCount, weedCount)}

	case "help":
		// 单次进入内完成 浇水/除草/除虫（对齐 Node visitFriendForHelp 单访多操作，减少进/出 RPC）
		var total int64
		rec := func(n int64, op string) {
			if n > 0 {
				total += n
				recordOperation(accountID, op, n)
			}
		}
		if len(analysis.NeedWater) > 0 {
			if err := execFriendOp(c, "WaterLand", proto.EncodeWaterLandRequest(analysis.NeedWater, gid)); err == nil {
				rec(int64(len(analysis.NeedWater)), "helpWater")
			}
		}
		if len(analysis.NeedWeed) > 0 {
			if err := execFriendOp(c, "WeedOut", proto.EncodeWeedOutRequest(analysis.NeedWeed, gid)); err == nil {
				rec(int64(len(analysis.NeedWeed)), "helpWeed")
			}
		}
		if len(analysis.NeedBug) > 0 {
			if err := execFriendOp(c, "Insecticide", proto.EncodeInsecticideRequest(analysis.NeedBug, gid)); err == nil {
				rec(int64(len(analysis.NeedBug)), "helpBug")
			}
		}
		// 经验上限检测已改为 exp 增量比对（detectExpFull），见 checkFriends 内 doHelp 循环
		if total == 0 {
			return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: 0, Message: "没有可帮忙土地"}
		}
		return &doFriendOperationResult{OK: true, OpType: opType, GID: gid, Count: total, Message: fmt.Sprintf("帮忙完成 %d 块", total)}

	default:
		return &doFriendOperationResult{OK: false, OpType: opType, GID: gid, Count: 0, Message: "未知操作类型"}
	}
}

// execFriendOp 执行好友农场操作；成功后从 reply 解析 operation_limits 刷新每日限制缓存
// （对齐 Node friend-operation-limits.js updateOperationLimits：偷=Harvest 在字段4，其余在字段2）
func execFriendOp(c *gw.Client, method string, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	rep, err := c.Request(ctx, plantService, method, body, 12*time.Second)
	if err == nil && rep != nil {
		updateOperationLimits(proto.DecodeOperationLimits(rep.Body))
	}
	return err
}

// friendLandDetail 好友地块展示信息（供 /api/friends/{gid}/lands）
type friendLandDetail struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	State    string `json:"state"`
	Img      string `json:"img,omitempty"`
	Progress int    `json:"progress"`
	TimeLeft string `json:"timeLeft"`
}

// getFriendLandsForDisplay 进入好友农场并解析地块明细（真实作物图）。
func getFriendLandsForDisplay(c *gw.Client, gid int64) ([]*friendLandDetail, error) {
	_, enterReply, err := enterFriendFarm(c, gid, 2, "")
	if err != nil {
		return nil, err
	}
	defer leaveFriendFarm(c, gid)
	cacheFriendDog(gid, enterReply)

	now := time.Now().Unix()
	lands := make([]*friendLandDetail, 0, len(enterReply.Lands))
	seen := map[int64]bool{}
	for _, l := range enterReply.Lands {
		if seen[l.ID] {
			continue
		}
		seen[l.ID] = true
		status, name, progress, timeLeft := analyzeLand(l, now)
		d := &friendLandDetail{
			ID:       l.ID,
			Name:     name,
			Status:   status,
			State:    iconFor(name),
			Progress: progress,
			TimeLeft: timeLeft,
		}
		// 真实作物图：Plant(id=种子ID) → seed_images_named/{id}_xxx.png
		if p := l.Plant; p != nil && p.ID > 0 {
			if img := GetItemImageURL(int(p.ID)); img != "" {
				d.Img = img
			}
		}
		lands = append(lands, d)
	}
	return lands, nil
}

// getFriendBasic 进入好友农场获取基本信息（姓名/等级/金币/头像）
func getFriendBasic(c *gw.Client, gid int64) *proto.VisitBasic {
	_, enterReply, err := enterFriendFarm(c, gid, 2, "")
	if err != nil {
		return nil
	}
	leaveFriendFarm(c, gid)
	return enterReply.Basic
}

// ============================================================
// 好友列表拉取（对齐 Node friend-api.js）：wx 用 GetAll；qq 用 GetGameFriends(已知GID) 回退 GetAll。
// ============================================================

// fetchAllFriends 拉取所有好友（对齐 Node getAllFriends）
// QQ 平台：GetGameFriends(已知GID) → 失败回退 GetAll
// 首次调用且有已知 GID 时，额外调用 VisitorList RPC 合并结果作为初始好友列表（去重）。
func fetchAllFriends(c *gw.Client, platform string, knownGids []int64) ([]*proto.GameFriend, error) {
	if platform == "qq" && len(knownGids) > 0 {
		// 对齐 Node fetchQqFriendsByKnownGids：按 35 一批分批请求 GetGameFriends，批次间随机延时 100-200ms
		const qqFriendListBatchSize = 35
		var all []*proto.GameFriend
		for i := 0; i < len(knownGids); i += qqFriendListBatchSize {
			end := i + qqFriendListBatchSize
			if end > len(knownGids) {
				end = len(knownGids)
			}
			batch := knownGids[i:end]
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			msg, err := c.Request(ctx, "gamepb.friendpb.FriendService", "GetGameFriends",
				proto.EncodeGetGameFriendsRequest(batch), 15*time.Second)
			cancel()
			if err == nil {
				all = append(all, proto.DecodeGetGameFriendsReply(msg.Body).Friends...)
			}
			// 批次间随机延时 100-200ms（对齐 Node randomDelay(100,200)）
			time.Sleep(time.Duration(100+rand.Intn(100)) * time.Millisecond)
		}
		if len(all) > 0 {
			return all, nil
		}
		// 全批失败回退 GetAll
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	msg, err := c.Request(ctx, "gamepb.friendpb.FriendService", "GetAll",
		proto.EncodeGetAllRequest(), 15*time.Second)
	if err != nil {
		return nil, err
	}
	return proto.DecodeGetAllReply(msg.Body).Friends, nil
}

// ============================================================
// 好友狗信息缓存（本地持久化，供前端"护主犬"徽标/筛选）
// ============================================================

type dogInfo struct {
	DogID   int64  `json:"dogId"`
	DogName string `json:"dogName"`
}

var dogCacheMu sync.Mutex

func dogCachePath(accountID string) string {
	return filepath.Join(dataDir, "friend_dogs_"+accountID+".json")
}

// cacheFriendDog 把进入好友农场解析出的狗信息写入本地缓存
func cacheFriendDog(gid int64, reply *proto.VisitEnterReply) {
	if reply == nil || gid <= 0 {
		return
	}
	name := reply.DogName
	if name == "" {
		name = "无狗"
	}
	dogCacheMu.Lock()
	defer dogCacheMu.Unlock()
	// accountID 取活跃默认账号
	accID := resolveAccountID("")
	if accID == "" {
		return
	}
	m, _ := readDogCache(accID)
	// 非护主犬（换狗/删好友后的伪护主犬残留）：删除旧缓存记录（对齐 Node friend-visit.js cacheDogInfoFromEnterReply）
	if reply.DogID != guardDogID {
		if _, ok := m[gid]; ok {
			delete(m, gid)
			writeDogCache(accID, m)
		}
		return
	}
	m[gid] = dogInfo{DogID: reply.DogID, DogName: name}
	writeDogCache(accID, m)
}

// handleFriendEnterError 分类处理进入好友农场失败（对齐 Node friend-api.js handleFriendEnterError）
// 返回处理类型："blacklist"（封禁→加黑名单） / "invalid_removed"（无效好友→移出已知列表） / ""（未处理）
func handleFriendEnterError(c *gw.Client, accountID string, gid int64, err error) string {
	msg := err.Error()
	// isEnterFarmBannedError：错误消息含 1002003 → 封禁，自动加黑名单
	if strings.Contains(msg, "1002003") {
		addFriendBlacklist(accountID, gid, fmt.Sprintf("GID:%d", gid))
		appendOpLog(accountID, "friend", fmt.Sprintf("检测到封禁好友 GID=%d，已自动加入黑名单", gid))
		return "blacklist"
	}
	// isInvalidFriendAccessError：code=1002002 硬匹配 或 关键词 → 失效/被删好友，自动移出已知列表
	if isInvalidFriendAccessErr(msg) {
		removeKnownFriendGid(accountID, gid)
		appendOpLog(accountID, "friend", fmt.Sprintf("好友 GID=%d 已失效/被删，自动移出已知好友列表", gid))
		return "invalid_removed"
	}
	return ""
}

// isInvalidFriendAccessErr 判断是否是「不是好友/无效好友」错误（对齐 Node isInvalidFriendAccessError）
func isInvalidFriendAccessErr(msg string) bool {
	// 错误码硬匹配：VisitService.Enter 返回 code=1002002「不是好友无法拜访」
	if strings.Contains(msg, "1002002") {
		return true
	}
	low := strings.ToLower(msg)
	for _, kw := range []string{"无效", "不存在", "删除", "关系", "not found", "invalid", "not friend", "friend"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// removeKnownFriendGid 从 config 已知好友列表移除失效 GID（对齐 Node removeKnownFriendGid）
func removeKnownFriendGid(accountID string, gid int64) {
	cfg := models.GetAccountConfig(accountID)
	if len(cfg.KnownFriendGIDs) == 0 {
		return
	}
	filtered := cfg.KnownFriendGIDs[:0]
	for _, g := range cfg.KnownFriendGIDs {
		if g != gid {
			filtered = append(filtered, g)
		}
	}
	if len(filtered) != len(cfg.KnownFriendGIDs) {
		cfg.KnownFriendGIDs = filtered
		_ = models.SetAccountConfig(accountID, cfg)
	}
}

func readDogCache(accountID string) (map[int64]dogInfo, error) {
	m := map[int64]dogInfo{}
	data, err := os.ReadFile(dogCachePath(accountID))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	return m, nil
}

func writeDogCache(accountID string, m map[int64]dogInfo) {
	data, _ := json.MarshalIndent(m, "", "  ")
	tmp := dogCachePath(accountID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, dogCachePath(accountID))
}

// getFriendDog 读取某好友的狗信息缓存（未访问过返回空）
func getFriendDog(accountID string, gid int64) (dogInfo, bool) {
	dogCacheMu.Lock()
	defer dogCacheMu.Unlock()
	m, err := readDogCache(accountID)
	if err != nil {
		_ = err
	}
	d, ok := m[gid]
	return d, ok
}

// ============================================================
// 好友黑名单本地库（对齐 Node getFriendBlacklist / addFriendToBlacklist，客户端侧管理）
// ============================================================

// blacklistEntry 黑名单条目（对齐 Node BlacklistItem：gid/name + skipSteal/skipHelp）
type blacklistEntry struct {
	GID       int64  `json:"gid"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
	AddedAt   string `json:"addedAt"`
	SkipSteal bool   `json:"skipSteal"`
	SkipHelp  bool   `json:"skipHelp"`
}

var blacklistMu sync.Mutex

func blacklistPath(accountID string) string {
	return filepath.Join(dataDir, "friend_blacklist_"+accountID+".json")
}

func readBlacklist(accountID string) map[int64]blacklistEntry {
	m := map[int64]blacklistEntry{}
	data, err := os.ReadFile(blacklistPath(accountID))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}

func writeBlacklist(accountID string, m map[int64]blacklistEntry) {
	data, _ := json.MarshalIndent(m, "", "  ")
	tmp := blacklistPath(accountID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, blacklistPath(accountID))
}

// getBlacklistEntries 返回黑名单条目（按加入时间倒序）
func getBlacklistEntries(accountID string) []blacklistEntry {
	blacklistMu.Lock()
	defer blacklistMu.Unlock()
	m := readBlacklist(accountID)
	out := make([]blacklistEntry, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	return out
}

// toggleBlacklist 拉黑/取消拉黑（对齐前端"已切换黑名单"语义）。
// 拉黑时默认 skipSteal=skipHelp=true（即黑名单内默认跳过偷菜与帮忙），
// 与 Node /api/friend-blacklist/toggle 的默认行为一致。
func toggleBlacklist(accountID string, gid int64, name string, skipSteal, skipHelp bool) (blacklisted bool, entry blacklistEntry) {
	blacklistMu.Lock()
	defer blacklistMu.Unlock()
	m := readBlacklist(accountID)
	if e, ok := m[gid]; ok {
		delete(m, gid)
		writeBlacklist(accountID, m)
		return false, e
	}
	e := blacklistEntry{
		GID:       gid,
		Name:      name,
		Reason:    "手动拉黑",
		AddedAt:   time.Now().Format("2006-01-02 15:04"),
		SkipSteal: skipSteal,
		SkipHelp:  skipHelp,
	}
	m[gid] = e
	writeBlacklist(accountID, m)
	return true, e
}

// updateBlacklistItem 更新黑名单条目的 skipSteal/skipHelp（对齐 Node /api/friend-blacklist/update）
func updateBlacklistItem(accountID string, gid int64, skipSteal, skipHelp bool) bool {
	blacklistMu.Lock()
	defer blacklistMu.Unlock()
	m := readBlacklist(accountID)
	e, ok := m[gid]
	if !ok {
		return false
	}
	e.SkipSteal = skipSteal
	e.SkipHelp = skipHelp
	m[gid] = e
	writeBlacklist(accountID, m)
	return true
}

// addFriendBlacklist 强制加入黑名单
func addFriendBlacklist(accountID string, gid int64, name string) {
	blacklistMu.Lock()
	defer blacklistMu.Unlock()
	m := readBlacklist(accountID)
	if _, ok := m[gid]; ok {
		return
	}
	m[gid] = blacklistEntry{GID: gid, Name: name, Reason: "手动拉黑", AddedAt: time.Now().Format("2006-01-02 15:04"), SkipSteal: true, SkipHelp: true}
	writeBlacklist(accountID, m)
}

// seedKnownFriendGidsFromVisitors 从访客记录获取初始好友 GID（对齐 Node syncKnownFriendGidsFromRecentVisitorsOnce）
func seedKnownFriendGidsFromVisitors(c *gw.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	// Try each RPC candidate for InteractRecords
	var records []*proto.InteractRecord
	for _, cand := range proto.InteractRecordCandidates {
		rep, err := c.Request(ctx, cand[0], cand[1], proto.EncodeInteractRecordsRequest(), 12*time.Second)
		if err == nil {
			records = proto.DecodeInteractRecordsReply(rep.Body)
			break
		}
	}
	if len(records) == 0 {
		return fmt.Errorf("no visitor records")
	}
	// 去重收集 visitorGid
	seen := map[int64]bool{}
	var gids []int64
	for _, r := range records {
		if r == nil || r.VisitorGID <= 0 {
			continue
		}
		if !seen[r.VisitorGID] {
			seen[r.VisitorGID] = true
			gids = append(gids, r.VisitorGID)
		}
	}
	if len(gids) == 0 {
		return fmt.Errorf("no visitor GIDs")
	}
	fmt.Printf("[friend] 首次登录从访客获取 %d 个好友GID\n", len(gids))
	return nil
}
