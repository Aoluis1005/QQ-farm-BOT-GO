package main

import (
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// 活动协议解析层。
// 字段号对齐 Node core/src/proto/activitypb.proto 与 core/src/services/activity.js 的 readProtoFields 手动解析。
// 关键：商店等数据嵌套在返回体深层，必须像 Node scanExchangeShopInfoFromRawBody 一样递归扫描字段块。

// --- 基础工具 ---

// actField 表示 length-delimited 协议中的一个字段条目
type actField struct {
	No     int
	Wire   int
	Varint int64
	Bytes  []byte
}

// readActFields 类似 Node readProtoFields：安全遍历（越界/异常直接停止返回已收集）
func readActFields(buf []byte) (out []actField) {
	defer func() { _ = recover() }()
	rd := proto.NewReader(buf)
	for rd.More() {
		f, w := rd.ReadTag()
		pf := actField{No: f, Wire: w}
		switch w {
		case 0:
			pf.Varint = rd.ReadInt64()
		case 2:
			pf.Bytes = rd.ReadBytes()
			if pf.Bytes == nil {
				return out
			}
		case 1:
			rd.ReadInt64()
			rd.ReadInt64()
		case 5:
			rd.ReadInt64()
		default:
			rd.Skip(w)
		}
		out = append(out, pf)
	}
	return out
}

func actNum(fs []actField, no int) int64 {
	for _, f := range fs {
		if f.No == no && f.Wire == 0 {
			return f.Varint
		}
	}
	return 0
}

func actBytes(fs []actField, no int) []byte {
	for _, f := range fs {
		if f.No == no && f.Wire == 2 {
			return f.Bytes
		}
	}
	return nil
}

func actBytesAll(fs []actField, no int) [][]byte {
	var out [][]byte
	for _, f := range fs {
		if f.No == no && f.Wire == 2 {
			out = append(out, f.Bytes)
		}
	}
	return out
}

func actStr(fs []actField, no int) string {
	b := actBytes(fs, no)
	if len(b) == 0 {
		return ""
	}
	return string(b)
}

// collectBytes 递归扫描 buf 中所有字段号为 target 的 length-delimited 块（对齐 Node scanLengthDelimitedFields）
func collectBytes(buf []byte, target int, maxDepth int) (out [][]byte) {
	defer func() { _ = recover() }()
	var rec func([]byte, int)
	rec = func(b []byte, depth int) {
		rd := proto.NewReader(b)
		for rd.More() {
			f, w := rd.ReadTag()
			if w == 2 {
				bb := rd.ReadBytes()
				if bb == nil {
					return
				}
				if f == target {
					out = append(out, bb)
				}
				if depth < maxDepth {
					rec(bb, depth+1)
				}
			} else {
				rd.Skip(w)
			}
		}
	}
	rec(buf, 0)
	return out
}

// --- 数据结构（供 JSON 输出） ---

// ActivityInfo 活动信息（对齐 Node normalizeHeluSubActivities 用到的字段）
type ActivityInfo struct {
	ID        int64  `json:"id"`
	ParentID  int64  `json:"parent_id"`
	Type      int64  `json:"type"`
	Title     string `json:"title"`
	Payload   string `json:"payload,omitempty"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Sort      int64  `json:"sort"`
	Visible   bool   `json:"visible"`
	Status    int64  `json:"status"`
	Enabled   bool   `json:"enabled"`
}

// ShopItem 兑换/随机商店商品项
type ShopItem struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Sort   int64  `json:"sort"`
	Status int64  `json:"status"`
	ItemID int64  `json:"item_id,omitempty"`
	Count  int64  `json:"count,omitempty"`
}

// ActivityNode 分组树节点
type ActivityNode struct {
	Info         *ActivityInfo    `json:"info"`
	Children     []*ActivityNode  `json:"children,omitempty"`
	ExchangeShop []*ShopItem      `json:"exchange_shop,omitempty"` // field102
	RandomShop   []*ShopItem      `json:"random_shop,omitempty"`   // field101
	HasDraw      bool             `json:"has_draw"`                // field105
	DrawInfo     []byte           `json:"-"`
}

// --- 解析函数 ---

func parseActivityInfo(fs []actField) *ActivityInfo {
	return &ActivityInfo{
		ID:        actNum(fs, 1),
		ParentID:  actNum(fs, 2),
		Type:      actNum(fs, 3),
		Title:     actStr(fs, 4),
		Payload:   actStr(fs, 5),
		StartTime: actNum(fs, 6),
		EndTime:   actNum(fs, 7),
		Sort:      actNum(fs, 8),
		Visible:   actNum(fs, 20) != 0,
		Status:    actNum(fs, 21),
		Enabled:   actNum(fs, 22) != 0,
	}
}

// parseShopItem 解析 corepb.Item（itemId=1,count=2）
func parseShopItem(fs []actField) (int64, int64) {
	return actNum(fs, 1), actNum(fs, 2)
}

// parseExchangeItems 解析 ExchangeShopInfo{items=1} → items
func parseExchangeItems(shopRaw []byte) []*ShopItem {
	var items []*ShopItem
	for _, blk := range collectBytes(shopRaw, 102, 8) {
		_ = blk
	}
	// 直接把 shopRaw 当 ExchangeShopInfo：items=1 每个是 ExchangeShopItem
	fs := readActFields(shopRaw)
	for _, raw := range actBytesAll(fs, 1) {
		it := parseShopItemFull(raw)
		if it != nil {
			items = append(items, it)
		}
	}
	return items
}

// parseShopItemFull 解析 ExchangeShopItem：1 id,2 item(corepb.Item),4 status,6 sort,7 name
func parseShopItemFull(raw []byte) *ShopItem {
	if len(raw) == 0 {
		return nil
	}
	fs := readActFields(raw)
	it := &ShopItem{ID: actNum(fs, 1), Status: actNum(fs, 4), Sort: actNum(fs, 6), Name: actStr(fs, 7)}
	if itemBytes := actBytes(fs, 2); len(itemBytes) > 0 {
		it.ItemID, it.Count = parseShopItem(readActFields(itemBytes))
	}
	return it
}

// ParseActivityList 解析 ActivityService.List 返回：
//
//	实测顶层仅 field1(单个大分组) + field2(repeated 精简活动条目)
//	entry: 1=id(varint) 2=title(string) 3=start(varint) 4=end(varint)
func ParseActivityList(body []byte) []*ActivityInfo {
	var out []*ActivityInfo
	defer func() { _ = recover() }()
	for _, entry := range actBytesAll(readActFields(body), 2) {
		fs := readActFields(entry)
		out = append(out, &ActivityInfo{
			ID:        actNum(fs, 1),
			Title:     actStr(fs, 2),
			StartTime: actNum(fs, 3),
			EndTime:   actNum(fs, 4),
		})
	}
	return out
}

// ParseActivityGroup 解析 ActivityService.GetGroup 返回：GetGroupReply{group=1 → ActivityNode}
// 递归遍历整棵树，并对每个节点扫描随机/兑换商店与抽奖数据。
func ParseActivityGroup(body []byte) *ActivityNode {
	defer func() { _ = recover() }()
	reply := readActFields(body)
	groupRaw := actBytes(reply, 1)
	if len(groupRaw) == 0 {
		return nil
	}
	return parseActivityNode(groupRaw)
}

func parseActivityNode(raw []byte) *ActivityNode {
	node := &ActivityNode{}
	fs := readActFields(raw)
	if infoRaw := actBytes(fs, 1); len(infoRaw) > 0 {
		node.Info = parseActivityInfo(readActFields(infoRaw))
	}
	for _, c := range actBytesAll(fs, 2) {
		node.Children = append(node.Children, parseActivityNode(c))
	}
	// 商店在节点 field102（ExchangeShopInfo）与 field101（RandomShopInfo）——数据或藏在深层，递归扫全量
	if shopBlk := actBytes(fs, 102); len(shopBlk) > 0 {
		node.ExchangeShop = parseExchangeItems(shopBlk)
	}
	if node.ExchangeShop == nil {
		// 深层回退：递归扫描所有 102 块解码（对齐 Node scanExchangeShopInfoFromRawBody）
		var best []*ShopItem
		for _, blk := range collectBytes(raw, 102, 8) {
			if items := parseExchangeItems(blk); len(items) > len(best) {
				best = items
			}
		}
		node.ExchangeShop = best
	}
	if drawRaw := actBytes(fs, 105); len(drawRaw) > 0 {
		node.HasDraw = true
		node.DrawInfo = drawRaw
	}
	return node
}

// ParseSeason 解析 SeasonService.GetSeasonInfo 返回（对齐 Node normalizeSeasonInfo/normalizeSeasonPassport）
type SeasonPassport struct {
	ActivityID       int64    `json:"activity_id"`
	CurrentLevel     int64    `json:"current_level"`
	Score            int64    `json:"score"`
	CurrentProgress  int64    `json:"current_progress"`
	NextLevelNeed    int64    `json:"next_level_need"`
	MaxLevel         int64    `json:"max_level"`
	FreeClaimedLevel int64    `json:"free_claimed_level"`
	PremiumClaimed   int64    `json:"premium_claimed_level"`
	Title            string   `json:"title"`
	ClaimableLevels  int64    `json:"claimable_levels"`
	RewardTiers      []*Tier  `json:"reward_tiers"`
}

type Tier struct {
	Level         int64    `json:"level"`
	FreeRewards   []*Item  `json:"free_rewards,omitempty"`
	PremiumRewards []*Item `json:"premium_rewards,omitempty"`
}

type Item struct {
	ItemID   int64  `json:"item_id"`
	Count    int64  `json:"count"`
	Name     string `json:"name"`
	Image    string `json:"image,omitempty"`
}

type SeasonInfo struct {
	Title        string          `json:"season_title"`
	Status       int64           `json:"status"`
	StartTime    int64           `json:"start_time"`
	EndTime      int64           `json:"end_time"`
	NowTime      int64           `json:"now_time"`
	ActivityID   int64           `json:"activity_id"`
	Passport     *SeasonPassport `json:"passport"`
}

func ParseSeason(body []byte) *SeasonInfo {
	defer func() { _ = recover() }()
	reply := readActFields(body)
	var out *SeasonInfo
	if seasonRaw := actBytes(reply, 1); len(seasonRaw) > 0 {
		fs := readActFields(seasonRaw)
		out = &SeasonInfo{
			Title:     actStr(fs, 2),
			Status:    actNum(fs, 3),
			StartTime: actNum(fs, 5),
			EndTime:   actNum(fs, 6),
			NowTime:   actNum(fs, 7),
		}
		if pRaw := actBytes(fs, 10); len(pRaw) > 0 {
			out.Passport = parseSeasonPassport(pRaw)
			if out.Passport != nil {
				out.ActivityID = out.Passport.ActivityID
			}
		}
	}
	return out
}

func parseSeasonPassport(raw []byte) *SeasonPassport {
	fs := readActFields(raw)
	p := &SeasonPassport{
		ActivityID:       actNum(fs, 1),
		CurrentLevel:     actNum(fs, 2),
		Score:            actNum(fs, 3),
		CurrentProgress:  actNum(fs, 4),
		NextLevelNeed:    actNum(fs, 5),
		MaxLevel:         actNum(fs, 6),
		FreeClaimedLevel: actNum(fs, 9),
		PremiumClaimed:   actNum(fs, 11),
		Title:            actStr(fs, 16),
	}
	p.ClaimableLevels = p.CurrentLevel - p.FreeClaimedLevel
	if p.ClaimableLevels < 0 {
		p.ClaimableLevels = 0
	}
	for _, tierRaw := range actBytesAll(fs, 8) {
		t := readActFields(tierRaw)
		tier := &Tier{Level: actNum(t, 1)}
		for _, r := range actBytesAll(t, 2) {
			if it := parseItem(r); it != nil {
				tier.FreeRewards = append(tier.FreeRewards, it)
			}
		}
		for _, r := range actBytesAll(t, 3) {
			if it := parseItem(r); it != nil {
				tier.PremiumRewards = append(tier.PremiumRewards, it)
			}
		}
		p.RewardTiers = append(p.RewardTiers, tier)
	}
	return p
}

// parseItem 解析 corepb.Item（对齐 Node parseActivityItemMessage）
func parseItem(raw []byte) *Item {
	fs := readActFields(raw)
	itemID := actNum(fs, 1)
	if itemID <= 0 {
		return nil
	}
	cnt := actNum(fs, 2)
	if cnt < 1 {
		cnt = 1
	}
	it := &Item{ItemID: itemID, Count: cnt, Name: itemDisplayName(itemID), Image: GetItemImageURL(int(itemID))}
	if it.Name == "" {
		it.Name = "物品" + itoa(itemID)
	}
	return it
}

// ParseSolar 解析 SolarTermsService.GetSolarTerms 返回（对齐 Node normalizeSolarTermsInfo）
type SolarTerm struct {
	ID       int64    `json:"id"`
	Status   int64    `json:"status"`
	Label    string   `json:"status_label"`
	Claimable bool    `json:"claimable"`
	Start    int64    `json:"start_time"`
	End      int64    `json:"end_time"`
	Title    string   `json:"title"`
	Rewards  []*Item  `json:"rewards,omitempty"`
}

type SolarInfo struct {
	NowTime         int64         `json:"now_time"`
	Terms           []*SolarTerm  `json:"terms"`
	ClaimableCount  int           `json:"claimable_count"`
	CurrentTermID   int64         `json:"current_term_id"`
	TipsText        string        `json:"tips_text"`
}

func ParseSolar(body []byte) *SolarInfo {
	defer func() { _ = recover() }()
	reply := readActFields(body)
	info := &SolarInfo{NowTime: actNum(reply, 2)}
	for _, termRaw := range actBytesAll(reply, 1) {
		t := readActFields(termRaw)
		term := &SolarTerm{
			ID:      actNum(t, 1),
			Status:  actNum(t, 2),
			Start:   actNum(t, 3),
			End:     actNum(t, 4),
			Title:   actStr(t, 6),
		}
		term.Claimable = term.Status == 2
		term.Label = solarStatusLabel(term.Status)
		for _, r := range actBytesAll(t, 5) {
			if it := parseItem(r); it != nil {
				term.Rewards = append(term.Rewards, it)
			}
		}
		if term.ID > 0 {
			info.Terms = append(info.Terms, term)
		}
	}
	for _, term := range info.Terms {
		if term.Claimable {
			info.CurrentTermID = term.ID
			info.ClaimableCount++
			if info.CurrentTermID == term.ID {
				break
			}
		}
	}
	// config 字段3 → tipsText(field3)
	if cfg := actBytes(reply, 3); len(cfg) > 0 {
		info.TipsText = actStr(readActFields(cfg), 3)
	}
	return info
}

func solarStatusLabel(status int64) string {
	switch status {
	case 2:
		return "可领取"
	case 3:
		return "已领取"
	case 1:
		return "未开启"
	case 5:
		return "已结束"
	default:
		return "状态" + itoa(status)
	}
}
