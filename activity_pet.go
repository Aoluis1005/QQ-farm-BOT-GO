package main

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Aoluis1005/go-farm-bot/proto"
)

// S3「萌宠成长日记」（比熊）活动协议层。
//
// 参考 protobuf（gamepb.activitypb）：
//   ActivityService.GetGroup(group_id=2026090100)
//     → GetGroupReply{ group=1: PetDiaryActivityGroup{ head=1, children=2[] } }
//       每个 children = PetDiaryActivityData{ head=1, shop=102, mega_event=110, pet_treasure_hunt=115 }
//   ActivityService.Operate{ activity_id=1, operate_type=2, <selector> }
//     operate_type 取值与参考实现的 OPERATIONS 一致（来自小程序 1.14.0.1 编码器）：
//       27 领养(finish_cg) / 29 投喂 / 30 寻宝 / 31 互动记录 / 32 领手记{order} /
//       41 刷锦囊 / 42 装锦囊 / 43 夺宝 / 44 被夺记录 / 45 开宝藏 / 46 被夺补偿 /
//       47 好友活动信息{friend_gid} / 48 领永久比熊 / 49 手记已读 / 50 跳过战斗CG
//     子活动：21 一键领种子礼包(id=2026090102) / 1 商店兑换(id=2026090103)
//
// 活动参数（CDN config/ActivityPetTreasureHuntBase，2026-09-10 线上实测）：
//   feed_items=1028:700 / growth_per_feed=700 / growth_adult_threshold=7000 / daily_feed_limit=16
const (
	petGroupID int64 = 2026090100
	petPetID   int64 = 2026090101
	petSeedsID int64 = 2026090102
	petShopID  int64 = 2026090103

	petFeedItemID int64 = 1028 // 萌宠元气糕
	petStarItemID int64 = 1029 // 幸运星

	petFeedCost           int64 = 700  // 每次投喂消耗元气糕
	petGrowthPerFeed      int64 = 700  // 每次投喂获得成长
	petAdultGrowth        int64 = 7000 // 成长到成年所需
	petDailyFeedLimit     int64 = 16
	petDailyTreasureLimit int64 = 10

	petOpInitialize int64 = 27
	petOpFeed       int64 = 29
	petOpDraw       int64 = 30
	petOpGetLog     int64 = 31
	petOpClaimStory int64 = 32
	petOpClaimDog   int64 = 48
	petOpSeeds      int64 = 21
	petOpShopBuy    int64 = 1

	// 爪印手记照片墙（Cocos 资源名 → 本地静态目录）
	petPhotoBase = "/game-config/pet-photos/"
)

// PetNurture 比熊养成状态
type PetNurture struct {
	Initialized bool  `json:"initialized"`
	Adult       bool  `json:"adult"`
	Stage       int64 `json:"stage"`
	Growth      int64 `json:"growth"`
	AdultGrowth int64 `json:"adultGrowth"`
	DogGranted  bool  `json:"dogGranted"`
	FeedCount   int64 `json:"feedCount"`
	FeedLimit   int64 `json:"feedLimit"`
	FeedCost    int64 `json:"feedCost"`
	CakeHave    int64 `json:"cakeHave"`
	CanFeed     bool  `json:"canFeed"`
}

// PetHunt 寻宝状态（成年后）
type PetHunt struct {
	Count   int64 `json:"count"`
	Limit   int64 `json:"limit"`
	Total   int64 `json:"total"`
	StarAll int64 `json:"starTotal"`
	Cost    int64 `json:"cost"`
	CanDraw bool  `json:"canDraw"`
}

// PetStory 爪印手记条目
type PetStory struct {
	Order    int64  `json:"order"`
	Unlocked bool   `json:"unlocked"`
	Claimed  bool   `json:"claimed"`
	Animated bool   `json:"animated"`
	Photo    string `json:"photo,omitempty"`
	Say      string `json:"say,omitempty"`
}

// PetState 比熊之家聚合状态
type PetState struct {
	Title       string      `json:"title"`
	Active      bool        `json:"active"`
	StartTime   int64       `json:"startTime"`
	EndTime     int64       `json:"endTime"`
	Cake        int64       `json:"cake"`
	Star        int64       `json:"star"`
	Nurture     PetNurture  `json:"nurture"`
	Hunt        PetHunt     `json:"hunt"`
	Stories     []*PetStory `json:"stories"`
	UnlockedNum int         `json:"unlockedCount"`
}

/* ---------- 解析 ---------- */

// petFetchGroupRaw 拉活动组原始回包（group_id=2026090100）
func petFetchGroupRaw(ctx context.Context, accountID string, timeout time.Duration) ([]byte, error) {
	b := proto.NewBuilder()
	b.FieldInt64(1, petGroupID)
	// 参考实现带一个空 uid（field2），保持一致
	b.FieldString(2, "")
	return rpcRequest(ctx, accountID, actSvc, "GetGroup", b.Bytes(), timeout)
}

// petParseGroupRaw 从 GetGroup 原始回包取出宠物节点（2026090101）的 pet 块与 head
func petParseGroupRaw(body []byte) (petRaw []byte, headRaw []byte) {
	groupRaw := actBytes(readActFields(body), 1)
	if len(groupRaw) == 0 {
		return nil, nil
	}
	gfs := readActFields(groupRaw)
	for _, childRaw := range actBytesAll(gfs, 2) {
		cfs := readActFields(childRaw)
		h := actBytes(cfs, 1)
		if actNum(readActFields(h), 1) != petPetID {
			continue
		}
		return actBytes(cfs, 115), h
	}
	// 兜底：本节点就是宠物节点
	if p := actBytes(gfs, 115); len(p) > 0 {
		return p, actBytes(gfs, 1)
	}
	return nil, nil
}

var petPhotoRe = regexp.MustCompile(`img_s3PhotoWall_(photo|say)(\d+)`)

// petPhotoURL 把 Cocos 资源路径转成本地静态 URL；取不到就返回空
func petPhotoURL(resPath string) string {
	m := petPhotoRe.FindStringSubmatch(resPath)
	if m == nil {
		return ""
	}
	return petPhotoBase + "s3photowall_" + m[1] + m[2] + ".png"
}

// petParseStories 解析爪印手记（story 块 field1 = repeated StoryInfo）
func petParseStories(petRaw []byte) []*PetStory {
	fs := readActFields(petRaw)
	storyBlk := actBytes(fs, 4)
	var out []*PetStory
	for _, raw := range actBytesAll(readActFields(storyBlk), 1) {
		sfs := readActFields(raw)
		st := &PetStory{
			Order:    actNum(sfs, 1),
			Unlocked: actNum(sfs, 2) != 0,
			Claimed:  actNum(sfs, 3) != 0,
			Animated: actNum(sfs, 5) != 0,
		}
		var desc struct {
			Photo string `json:"photo"`
			Say   string `json:"say"`
		}
		if err := json.Unmarshal([]byte(actStr(sfs, 4)), &desc); err == nil {
			st.Photo = petPhotoURL(desc.Photo)
			st.Say = petPhotoURL(desc.Say)
		}
		out = append(out, st)
	}
	return out
}

// petReadItems 读背包里指定物品的数量
func petReadItems(ctx context.Context, accountID string, ids ...int64) map[int64]int64 {
	out := map[int64]int64{}
	for _, id := range ids {
		out[id] = 0
	}
	c, err := clientPool.Get(accountID)
	if err != nil {
		return out
	}
	rep, err := c.Request(ctx, "gamepb.itempb.ItemService", "Bag", proto.EncodeBagRequest(), 12*time.Second)
	if err != nil {
		return out
	}
	for _, it := range proto.DecodeBagReply(rep.Body).Items {
		if _, want := out[it.ID]; want {
			out[it.ID] += it.Count
		}
	}
	return out
}

// petBuildState 组装比熊之家状态
func petBuildState(ctx context.Context, accountID string, body []byte) *PetState {
	petRaw, headRaw := petParseGroupRaw(body)
	hfs := readActFields(headRaw)
	st := &PetState{
		Title:     actStr(hfs, 4),
		StartTime: actNum(hfs, 6),
		EndTime:   actNum(hfs, 7),
	}
	if st.Title == "" {
		st.Title = "萌宠成长日记"
	}
	now := time.Now().Unix()
	st.Active = st.StartTime <= now && (st.EndTime == 0 || now <= st.EndTime)

	fs := readActFields(petRaw)
	nfs := readActFields(actBytes(fs, 1))
	ffs := readActFields(actBytes(fs, 2))
	huntFs := readActFields(actBytes(fs, 3))

	nur := PetNurture{
		Initialized: actNum(nfs, 1) != 0, // cg_played
		Stage:       actNum(nfs, 4),
		Growth:      actNum(nfs, 3),
		AdultGrowth: petAdultGrowth,
		DogGranted:  actNum(nfs, 6) != 0,
		FeedCount:   actNum(ffs, 1),
		FeedLimit:   petDailyFeedLimit,
		FeedCost:    petFeedCost,
	}
	nur.Adult = nur.Stage == 2

	bag := petReadItems(ctx, accountID, petFeedItemID, petStarItemID)
	nur.CakeHave = bag[petFeedItemID]
	nur.CanFeed = st.Active && !nur.Adult && nur.Stage == 1 &&
		nur.FeedCount < nur.FeedLimit && nur.CakeHave >= petFeedCost

	st.Cake = bag[petFeedItemID]
	st.Star = bag[petStarItemID]
	st.Nurture = nur
	st.Hunt = PetHunt{
		Count:   actNum(huntFs, 1),
		Total:   actNum(huntFs, 2),
		StarAll: actNum(huntFs, 3),
		Limit:   petDailyTreasureLimit,
		Cost:    petFeedCost,
	}
	st.Hunt.CanDraw = st.Active && nur.Adult && st.Hunt.Count < st.Hunt.Limit && st.Cake >= petFeedCost
	st.Stories = petParseStories(petRaw)
	for _, s := range st.Stories {
		if s.Unlocked {
			st.UnlockedNum++
		}
	}
	return st
}

/* ---------- API ---------- */

// 路由注册在 activity_api.go 的 registerActivityAPI 内（/api/activity/pet 与 /api/activity/pet/operate）

// GET /api/activity/pet?accountId=X —— 比熊之家 + 爪印手记状态
func handleActivityPet(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	body, err := petFetchGroupRaw(ctx, accountID, 20*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	st := petBuildState(ctx, accountID, body)
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID, "id": petPetID, "groupId": petGroupID, "data": st,
	})
}

// POST /api/activity/pet/operate  body: {accountId, action, order?}
// action: initialize(领养) / feed(投喂) / draw(寻宝) / claimDog(领永久比熊) /
//         story(领手记, 需 order) / seeds(一键领种子礼包) / exchange(商店兑换, 需 goodsId/count)
func handleActivityPetOperate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"accountId"`
		Action    string `json:"action"`
		Order     int64  `json:"order"`
		GoodsID   int64  `json:"goodsId"`
		Count     int64  `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONMap(w, "ok", false, "error", "参数解析失败")
		return
	}
	accountID := resolveAccountID(req.AccountID)
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()

	b := proto.NewBuilder()
	switch req.Action {
	case "initialize":
		b.FieldInt64(1, petPetID)
		b.FieldInt64(2, petOpInitialize)
	case "feed":
		b.FieldInt64(1, petPetID)
		b.FieldInt64(2, petOpFeed)
	case "draw":
		b.FieldInt64(1, petPetID)
		b.FieldInt64(2, petOpDraw)
	case "claimDog":
		b.FieldInt64(1, petPetID)
		b.FieldInt64(2, petOpClaimDog)
	case "story":
		if req.Order <= 0 {
			writeJSONMap(w, "ok", false, "error", "缺少手记编号 order")
			return
		}
		sub := proto.NewBuilder()
		sub.FieldInt64(1, req.Order)
		b.FieldInt64(1, petPetID)
		b.FieldInt64(2, petOpClaimStory)
		b.FieldMessage(132, sub.Bytes())
	case "seeds":
		b.FieldInt64(1, petSeedsID)
		b.FieldInt64(2, petOpSeeds)
	case "exchange":
		if req.GoodsID <= 0 {
			writeJSONMap(w, "ok", false, "error", "缺少 goodsId")
			return
		}
		cnt := req.Count
		if cnt <= 0 {
			cnt = 1
		}
		sub := proto.NewBuilder()
		sub.FieldInt64(1, req.GoodsID)
		sub.FieldInt64(2, cnt)
		b.FieldInt64(1, petShopID)
		b.FieldInt64(2, petOpShopBuy)
		b.FieldMessage(101, sub.Bytes())
	default:
		writeJSONMap(w, "ok", false, "error", "未知操作: "+req.Action)
		return
	}

	if _, err := rpcRequest(ctx, accountID, actSvc, "Operate", b.Bytes(), 20*time.Second); err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	// 操作后回读状态（顺带回前端刷新）
	body, err := petFetchGroupRaw(ctx, accountID, 20*time.Second)
	if err != nil {
		writeJSONMap(w, "ok", true, "account", accountID, "action", req.Action, "warning", actErrMsg(err))
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID, "action": req.Action,
		"data": petBuildState(ctx, accountID, body),
	})
}

// 保留：从 group 里取子节点 head 名（活动名统一由服务端 head.name 提供）
func petNodeName(headRaw []byte) string {
	return strings.TrimSpace(actStr(readActFields(headRaw), 4))
}

var _ = strconv.Itoa
