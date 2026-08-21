package main

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/Aoluis1005/go-farm-bot/proto"
)

// 活动协议解析层。
// 字段号。
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

// collectBytes 递归扫描 buf 中所有字段号为 target 的 length-delimited 块
// 注意：必须与 readActFields 采用相同的显式逐 wire 读取方式，否则会漏扫嵌套字段（此前用 Skip 漏掉 field110）。
func collectBytes(buf []byte, target int, maxDepth int) (out [][]byte) {
	defer func() { _ = recover() }()
	var rec func([]byte, int)
	rec = func(b []byte, depth int) {
		rd := proto.NewReader(b)
		for rd.More() {
			f, w := rd.ReadTag()
			switch w {
			case 2:
				bb := rd.ReadBytes()
				if bb == nil {
					return
				}
				if f == target {
					out = append(out, bb)
				}
				rec(bb, depth+1) // 始终递归，靠 recover 兜底，避免深度限制漏扫
			case 0:
				rd.ReadInt64()
			case 1:
				rd.ReadInt64()
				rd.ReadInt64()
			case 5:
				rd.ReadInt64()
			default:
				rd.Skip(w)
			}
		}
	}
	rec(buf, 0)
	return out
}

// --- 数据结构（供 JSON 输出） ---

// ActivityInfo 活动信息
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
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Sort         int64  `json:"sort"`
	Status       int64  `json:"status"`
	StatusLabel  string `json:"status_label,omitempty"`
	Owned        bool   `json:"owned"`
	IsRepeatable bool   `json:"is_repeatable,omitempty"` // 化肥/道具：可重复兑换（type 7 或 fertilizer/fertilizerpro）
	ExchangeLimit int64 `json:"exchange_limit,omitempty"`  // 可重复道具=剩余可兑换次数
	ItemID       int64  `json:"item_id,omitempty"`
	Count        int64  `json:"count,omitempty"`
	Image        string `json:"image,omitempty"`
	CurrencyID   int64  `json:"currency_id,omitempty"` // cost.itemId（星砂=1023）
	CurrencyName string `json:"currency_name,omitempty"`
	Price        int64  `json:"price,omitempty"`        // cost.count
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

// parseShopItemFull 解析 ExchangeShopItem：1 id, 2 item(corepb.Item), 3 cost(价格 Item), 4 status, 5 owned, 6 sort, 7 name
func parseShopItemFull(raw []byte) *ShopItem {
	if len(raw) == 0 {
		return nil
	}
	fs := readActFields(raw)
	it := &ShopItem{ID: actNum(fs, 1), Status: actNum(fs, 4), Sort: actNum(fs, 6), Name: actStr(fs, 7)}
	if itemBytes := actBytes(fs, 2); len(itemBytes) > 0 {
		it.ItemID, it.Count = parseShopItem(readActFields(itemBytes))
	}
	if costBytes := actBytes(fs, 3); len(costBytes) > 0 {
		cf := readActFields(costBytes)
		it.CurrencyID = actNum(cf, 1)
		it.Price = actNum(cf, 2)
	}
	it.Owned = actNum(fs, 5) != 0
	it.IsRepeatable = isRepeatableItem(it.ItemID)
	it.Image = GetItemImageURL(int(it.ItemID))
	// 可重复道具（化肥）：status 即剩余可兑换次数，且不因 owned 阻塞
	if it.IsRepeatable && it.Status > 1 {
		it.ExchangeLimit = it.Status
	}
	if it.CurrencyID > 0 {
		it.CurrencyName = itemDisplayName(it.CurrencyID)
	}
	it.StatusLabel = exchangeShopStatusLabel(it)
	return it
}

// isRepeatableItem 判定可重复兑换道具
func isRepeatableItem(itemID int64) bool {
	if it, ok := itemInfoMap[int(itemID)]; ok {
		if it.Type == 7 {
			return true
		}
		switch it.InteractionType {
		case "fertilizer", "fertilizerpro":
			return true
		}
	}
	return false
}

func exchangeShopStatusLabel(it *ShopItem) string {
	// 可重复道具不限购拥有，始终可兑换；一次性道具（装扮）已拥有则不可再兑
	if !it.IsRepeatable && it.Owned {
		return "已拥有"
	}
	if it.ExchangeLimit > 0 {
		return "可兑换"
	}
	switch it.Status {
	case 3:
		return "已售"
	case 5:
		return "特殊商品"
	default:
		return "可兑换"
	}
}

// ParseActivityList 解析 ActivityService.List 返回：
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
		// 深层回退：递归扫描所有 102 块解码
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

// ParseSeason 解析 SeasonService.GetSeasonInfo 返回
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

// parseItem 解析 corepb.Item
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

// ParseSolar 解析 SolarTermsService.GetSolarTerms 返回
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

func timeNow() int64 { return time.Now().Unix() }

func jsonUnmarshalMap(s string) map[string]interface{} {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

func strAny(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ---- 观星礼录（二十八星宿） ----
// 数据源：ActivityService.GetGroup(GUANXING_ACTIVITY_ID) 返回体 field110（CONSTELLATION_DATA_FIELD）。

const (
	guanxingActivityID  = 2026072701 // 观星礼录本体（type=13）
	guanxingClaimCmd    = 21         // 一键领取全部已解锁星宿
	constellationDataFi = 110        // ActivityData 中星宿数据所在字段号
	guanxingNoReward    = 1034038    // 无可领取奖励节点（幂等信号）
	guanxingExtField    = 119        // 官方客户端点亮请求附带的空扩展字段
)

// ConstellationGroup 星宿分组：名称、四象归类与释义
type ConstellationGroup struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Explain  string `json:"explain"`
	Links    string `json:"links"`
}

// ConstellationNode 星宿节点
// field2=已解锁 field3=已领取 field4=可领取
type ConstellationNode struct {
	ID          int64   `json:"id"`
	Day         int64   `json:"day"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Explain     string  `json:"explain"`
	Unlocked    bool    `json:"unlocked"`
	Claimed     bool    `json:"claimed"`
	Claimable   bool    `json:"claimable"`
	StatusLabel string  `json:"status_label"`
	Rewards     []*Item `json:"rewards,omitempty"`
}

type ConstellationSummary struct {
	TotalDays      int    `json:"total_days"`
	CurrentDay     int64  `json:"current_day"`
	UnlockedCount  int    `json:"unlocked_count"`
	ClaimedCount   int    `json:"claimed_count"`
	ClaimableCount int    `json:"claimable_count"`
	PendingRewards []*Item `json:"pending_rewards"`
}

type ConstellationInfo struct {
	ActivityID  int64                `json:"activity_id"`
	Title       string               `json:"title"`
	SeasonTitle  string               `json:"seasonTitle"` // 
	StartTime   int64                `json:"start_time"`
	EndTime     int64                `json:"end_time"`
	NowTime     int64                `json:"now_time"`
	CurrentDay  int64                `json:"current_day"`
	TotalDays   int                  `json:"total_days"`
	Nodes       []*ConstellationNode `json:"nodes"`
	Summary     ConstellationSummary `json:"summary"`
	Warning     string               `json:"warning,omitempty"`
}

// parseConstGroup 解析星宿分组（field:1=id,3=name,4=links,5=configText(JSON category/explain)）
func parseConstGroup(raw []byte) *ConstellationGroup {
	fs := readActFields(raw)
	id := actNum(fs, 1)
	if id <= 0 {
		return nil
	}
	g := &ConstellationGroup{ID: id, Name: actStr(fs, 3), Links: actStr(fs, 4)}
	if cfg := actStr(fs, 5); cfg != "" {
		if obj := jsonUnmarshalMap(cfg); obj != nil {
			g.Category = strAny(obj["category"])
			g.Explain = strAny(obj["explain"])
		}
	}
	return g
}

// parseConstNode 解析星宿节点（field:1=id,2=unlocked,3=claimed,4=claimable,5=rewards）
func parseConstNode(raw []byte, gmap map[int64]*ConstellationGroup) *ConstellationNode {
	fs := readActFields(raw)
	id := actNum(fs, 1)
	if id <= 0 {
		return nil
	}
	unlocked := actNum(fs, 2) == 1
	claimed := actNum(fs, 3) == 1
	claimable := actNum(fs, 4) == 1
	n := &ConstellationNode{
		ID: id, Day: id, Unlocked: unlocked, Claimed: claimed, Claimable: claimable,
	}
	if g := gmap[id]; g != nil {
		n.Name, n.Category, n.Explain = g.Name, g.Category, g.Explain
	}
	if n.Name == "" {
		n.Name = "第" + itoa(id) + "宿"
	}
	switch {
	case claimed:
		n.StatusLabel = "已领取"
	case claimable:
		n.StatusLabel = "可领取"
	case unlocked:
		n.StatusLabel = "已解锁"
	default:
		n.StatusLabel = "未解锁"
	}
	for _, r := range actBytesAll(fs, 5) {
		if it := parseItem(r); it != nil {
			n.Rewards = append(n.Rewards, it)
		}
	}
	return n
}

// findConstellationBytes 提取返回体最大 field110 星宿块。
// 遍历方式与 dbgCollectStats 完全一致（含深度≤6），实测可靠拿到 field110。
func findConstellationBytes(body []byte) []byte {
	var best []byte
	var rec func([]byte, int)
	rec = func(b []byte, depth int) {
		if len(b) == 0 || depth > 6 {
			return
		}
		defer func() { _ = recover() }()
		rd := proto.NewReader(b)
		for rd.More() {
			f, w := rd.ReadTag()
			switch w {
			case 2:
				nb := rd.ReadBytes()
				if nb == nil {
					return
				}
				if f == constellationDataFi && len(nb) > len(best) {
					best = make([]byte, len(nb))
					copy(best, nb)
				}
				rec(nb, depth+1)
			case 0:
				rd.ReadInt64()
			case 1:
				rd.ReadInt64()
				rd.ReadInt64()
			case 5:
				rd.ReadInt64()
			default:
				rd.Skip(w)
			}
		}
	}
	rec(body, 0)
	return best
}

// ParseConstellation 从 GetGroup 原始返回体中提取 field110 星宿数据
func ParseConstellation(body []byte) *ConstellationInfo {
	defer func() { _ = recover() }()
	now := timeNow()
	base := &ConstellationInfo{ActivityID: guanxingActivityID, Title: "观星礼录", SeasonTitle: "观星礼录", StartTime: 0, EndTime: 0, NowTime: now}
	// 定位 activity info（ActivityInfo: 1=id 4=title 6=start 7=end）
	for _, blk := range collectBytes(body, 1, 5) {
		fs := readActFields(blk)
		if actNum(fs, 1) != guanxingActivityID {
			continue
		}
		if actStr(fs, 4) == "" {
			continue
		}
		base.StartTime = actNum(fs, 6)
		base.EndTime = actNum(fs, 7)
		base.Title = actStr(fs, 4)
		break
	}
	// 找 field110 星宿数据块（取最大）
	best := findConstellationBytes(body)
	if len(best) == 0 {
		base.Warning = "未解析到星宿数据"
		return base
	}
	fs := readActFields(best)
	gmap := map[int64]*ConstellationGroup{}
	for _, gRaw := range actBytesAll(fs, 5) {
		if g := parseConstGroup(gRaw); g != nil {
			gmap[g.ID] = g
		}
	}
	for _, nRaw := range actBytesAll(fs, 4) {
		if n := parseConstNode(nRaw, gmap); n != nil {
			base.Nodes = append(base.Nodes, n)
		}
	}
	// 按 id 排序
	sort.Slice(base.Nodes, func(i, j int) bool { return base.Nodes[i].ID < base.Nodes[j].ID })
	base.TotalDays = len(base.Nodes)
	serverDay := actNum(fs, 1)
	if serverDay > 0 {
		base.CurrentDay = serverDay
	} else if base.StartTime > 0 && now > base.StartTime {
		d := (now - base.StartTime) / 86400 + 1
		if d < 1 {
			d = 1
		}
		if base.TotalDays > 0 && d > int64(base.TotalDays) {
			d = int64(base.TotalDays)
		}
		base.CurrentDay = d
	}
	var pending []*Item
	for _, n := range base.Nodes {
		if n.Unlocked {
			base.Summary.UnlockedCount++
		}
		if n.Claimed {
			base.Summary.ClaimedCount++
		}
		if n.Claimable {
			base.Summary.ClaimableCount++
			pending = append(pending, n.Rewards...)
		}
	}
	base.Summary.TotalDays = base.TotalDays
	base.Summary.CurrentDay = base.CurrentDay
	base.Summary.PendingRewards = mergeRewardItems(pending)
	return base
}

func mergeRewardItems(items []*Item) []*Item {
	merged := []*Item{}
	idx := map[int64]int{}
	for _, it := range items {
		if it == nil || it.ItemID <= 0 {
			continue
		}
		if i, ok := idx[it.ItemID]; ok {
			merged[i].Count += it.Count
			continue
		}
		merged = append(merged, it)
		idx[it.ItemID] = len(merged) - 1
	}
	return merged
}

// ---- 领取结果解析 ----

// ParseSeasonClaim 解析 SeasonService.ClaimBattlePassRewards 返回
// field1=rewards(repeated Item) field3=passport
type SeasonClaimResult struct {
	Rewards  []*Item         `json:"rewards"`
	Passport *SeasonPassport `json:"passport,omitempty"`
}

func ParseSeasonClaim(body []byte) *SeasonClaimResult {
	defer func() { _ = recover() }()
	fs := readActFields(body)
	res := &SeasonClaimResult{}
	for _, r := range actBytesAll(fs, 1) {
		if it := parseItem(r); it != nil {
			res.Rewards = append(res.Rewards, it)
		}
	}
	if p := actBytes(fs, 3); len(p) > 0 {
		res.Passport = parseSeasonPassport(p)
	}
	return res
}

// ParseSolarClaim 解析 SolarTermsService.ClaimSolarTerms 返回
// field1=rewards field2=term
type SolarClaimResult struct {
	Rewards []*Item     `json:"rewards"`
	Term    *SolarTerm  `json:"term,omitempty"`
}

func ParseSolarClaim(body []byte) *SolarClaimResult {
	defer func() { _ = recover() }()
	fs := readActFields(body)
	res := &SolarClaimResult{}
	for _, r := range actBytesAll(fs, 1) {
		if it := parseItem(r); it != nil {
			res.Rewards = append(res.Rewards, it)
		}
	}
	if t := actBytes(fs, 2); len(t) > 0 {
		termFS := readActFields(t)
		term := &SolarTerm{ID: actNum(termFS, 1), Status: actNum(termFS, 2), Start: actNum(termFS, 3), End: actNum(termFS, 4), Title: actStr(termFS, 6)}
		term.Claimable = term.Status == 2
		term.Label = solarStatusLabel(term.Status)
		for _, r := range actBytesAll(termFS, 5) {
			if it := parseItem(r); it != nil {
				term.Rewards = append(term.Rewards, it)
			}
		}
		res.Term = term
	}
	return res
}

// appendVarintBytes 将 varint 编码写入字节切片（LEN 字段内嵌小整数用，如喷洒 land_id）
func appendVarintBytes(v int64) []byte {
	var buf []byte
	u := uint64(v)
	for u >= 0x80 {
		buf = append(buf, byte(u)|0x80)
		u >>= 7
	}
	return append(buf, byte(u))
}
