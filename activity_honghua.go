package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ===== 公益小红花（CharityRedFlower）活动 =====
// 活动根 2026090900，子节点 2026090901（捐赠/领取落此节点，抓包确认）。
// 实现原则（2026-08-31）：能做的真接，猜的标注。
//   - 已实装：送出爱心值（Operate 2026090901 cmd=36，抓包实锤）、
//     送出公益金（Operate 2026090901 cmd=38，抓包实锤，单账号活动期仅 1 次资格 + 真实 1 元扣款，做已捐防重）、
//     领取奖励（cmd 为推断，见下方常量，?cmd 可覆盖便于线上试）。
//   - 领取 cmd 已于 2026-09-01 线上逐 cmd 实测确认（cmd=35 分享奖励 / cmd=39 爱心值档位）。
//
// 捐赠/领取请求结构（抓包实锤）：ActivityService.Operate{ field1=activity_id, field2=cmd }
//   （真实客户端还会带 field3=活动配置回显块，服务端侧应有自身配置，本实现先省略 field3，失败再补）。

const (
	honghuaActivityID = 2026090901 // 捐赠/领取子节点（抓包确认 Operate 落此节点）

	honghuaLoveCmd = 36 // 送出爱心值（抓包实锤 2026-08-31）
	honghuaFundCmd = 38 // 送出公益金（抓包实锤 2026-08-31，单账号仅 1 次资格 + 真实 1 元）

	// ===== 领取奖励 cmd（【推断】2026-08-31，抓包未抓到，线上用 ?cmd 覆盖试）=====
	// 捐赠已确认 36/38，领取按同活动 cmd 邻接规律推断：
	//   37 = 领取公益礼包（每日，收获小红花后领，化肥1h×2）
	//   39 = 领取个人爱心值档位奖励（30/60/90/120/150，参数待确认）
	//   40 = 领取全服公益结算礼包（化肥包×20/金豆豆×200/点券×300）
	// 2026-09-01 线上逐 cmd 实测实锤（扫 cmd 30~45，仅以下 4 个 cmd 有效，其余一律「活动参数错误」）：
	//   cmd=35 领分享奖励  → 「当日分享奖励不可领取」
	//   cmd=36 送爱心值    → 「爱心不足，收获小红花果实可获得哦~」
	//   cmd=38 送公益金    → 「当日未收获小红花，无法送出公益金」
	//   cmd=39 领爱心值档位奖励 → 成功（回包 f139 空块）
	honghuaClaimShareCmd = 35 // 领取分享奖励（实测有效）
	honghuaClaimTierCmd  = 39 // 领取个人爱心值档位奖励（实测有效，30/60/90/120/150 五档）
	// 以下两个 cmd 在 30~45 区间内实测无效（均返回「活动参数错误」），保留为待确认：
	// 每日公益礼包 / 全服结算礼包 的操作可能尚未开放，或 cmd 不在该区间。
	honghuaClaimDailyCmd  = 37
	honghuaClaimSettleCmd = 40

	// honghuaExtBase 扩展参数字段号基准（2026-09-01 实测实锤）：
	//   字段号 = cmd + 99（35→134, 36→135, 38→137, 39→138 全部吻合）。
	// 请求形状：Operate{ f1=2026090901, f2=cmd, f<cmd+99>={} }，扩展块为**空子消息**且必带，
	// 缺失或字段号不匹配一律返回「活动参数错误」。
	honghuaExtBase = 99

	honghuaRewardItemOrganic = 80013 // 有机化肥（8 小时）
	honghuaRewardItemCoupon  = 1002  // 点券
	honghuaRewardItemFrame   = 2158  // 公益小红花做好事头像框
	// honghuaGiftItem 公益礼包奖励 = 化肥(1小时)。
	// 物品表 ItemInfo.json：80001=化肥(1小时)、80011=有机化肥(1小时)。
	// 抓包实锤：cmd=38 送出公益金后，回包 f138.f3 = {80001, 2}，紧随的 ItemNotify 也是化肥(1小时) +2，
	// 与规则「公益礼包：礼包内含化肥（1小时）* 2，每日限领 1 次」完全吻合
	// —— 公益礼包就是送出公益金时一并到账的奖励，不是独立领取操作。
	honghuaGiftItem = 80001
)

// honghuaTier 个人爱心值档位奖励（抓包 f116 实锤：阈值→奖励）。
type honghuaTier struct {
	Threshold int64  `json:"threshold"` // 累计爱心值门槛
	ItemID    int64  `json:"itemId"`    // 奖励物品 id
	ItemName  string `json:"itemName"`
	Count     int64  `json:"count"` // 奖励数量
}

// honghuaTiers 5 档（f116 f9 repeated：30/60/90/120/150）。
var honghuaTiers = []honghuaTier{
	{Threshold: 30, ItemID: honghuaRewardItemOrganic, ItemName: "有机化肥(8小时)", Count: 1},
	{Threshold: 60, ItemID: honghuaRewardItemCoupon, ItemName: "点券", Count: 50},
	{Threshold: 90, ItemID: honghuaRewardItemOrganic, ItemName: "有机化肥(8小时)", Count: 2},
	{Threshold: 120, ItemID: honghuaRewardItemCoupon, ItemName: "点券", Count: 100},
	{Threshold: 150, ItemID: honghuaRewardItemFrame, ItemName: "公益小红花做好事头像框", Count: 1},
}

// honghuaItemName 小红花活动物品名。
func honghuaItemName(id int64) string {
	switch id {
	case honghuaRewardItemOrganic:
		return "有机化肥(8小时)"
	case honghuaRewardItemCoupon:
		return "点券"
	case honghuaRewardItemFrame:
		return "公益小红花做好事头像框"
	// 80001 在物品表(ItemInfo.json)里是「化肥(1小时)」，此前误当成公益金。
	// 送公益金(cmd=38)后回包 f138.f3 给的就是 {80001 化肥(1小时), 2}，即规则里的公益礼包。
	case honghuaGiftItem:
		return "化肥(1小时)"
	case 101604:
		return "金豆豆"
	}
	return itemDisplayName(id)
}

// honghuaSeedID / honghuaFruitID 小红花种子/果实物品 id（CDN ItemInfo 实锤：20883 种子 / 40883 果实）。
const (
	honghuaSeedID  = 20883
	honghuaFruitID = 40883
)

// honghuaSeedName 小红花种子展示名（从同步的 ItemInfo 动态读取, 无则兜底）。
func honghuaSeedName() string {
	if n := itemDisplayName(honghuaSeedID); n != "" {
		return n
	}
	return "小红花种子"
}

// honghuaFruitName 小红花果实展示名。
func honghuaFruitName() string {
	if n := itemDisplayName(honghuaFruitID); n != "" {
		return n
	}
	return "小红花"
}

// ===== 公益金已捐防重（单账号活动期仅 1 次资格）=====
// 服务端已捐状态未知（group 字段未确认），用内存记录「今日已捐」做 best-effort 防重；
// 重启丢失可接受（与 qingmei 领种子标记一致）。真实 1 元扣款，重复捐会真扣钱，必须拦。
var (
	honghuaFundMu   sync.Mutex
	honghuaFundDate = map[string]string{} // accountID -> YYYYMMDD（已成功送出公益金）
)

func honghuaFundMarkedToday(accountID string) bool {
	honghuaFundMu.Lock()
	defer honghuaFundMu.Unlock()
	return honghuaFundDate[accountID] == time.Now().Format("20060102")
}

func honghuaFundMark(accountID string) {
	honghuaFundMu.Lock()
	defer honghuaFundMu.Unlock()
	honghuaFundDate[accountID] = time.Now().Format("20060102")
}

// honghuaF3ForCmd 返回某 cmd 对应的真实 field3 配置块（抓包实锤，真实客户端必带）。
// 默认用 love 块；love/fund 分别用各自抓到的块（差异 3 字节，按 cmd 精确匹配最稳）。
func honghuaF3ForCmd(cmd int64) []byte {
	s := honghuaF3LoveB64
	switch cmd {
	case honghuaFundCmd:
		s = honghuaF3FundB64
	}
	if s == "" {
		return nil // 无真实数据时省略 field3，避免发送空配置块
	}
	if dec, e := base64.StdEncoding.DecodeString(s); e == nil {
		return dec
	}
	return nil
}

// honghuaOperate 组装公益小红花 Operate 请求，返回原始回包 body。
// 形状与已跑通的「雨落成诗」一致（activity_yulu.go）：
//   ActivityService.Operate{ field1 = 活动节点 id, field2 = cmd, field<ext> = 扩展参数子消息 }
// 请求体出网关时由 gw/client.go 的 ACE 统一加密，这里只负责拼明文 protobuf。
//
// 说明（2026-08-31 更正）：此前把抓包里的 ACE 密文当 protobuf 解析，误得出
// 「顶层只有 field1325 fixed64」的结论——那是密文，不是明文。真实明文按 yulu 形状拼。
//
// extBody 非空时按「扩展参数子消息」附加到 extField 号上（debug 可调，默认 140）。
func honghuaOperate(ctx context.Context, accountID string, cmd int64, extBody []byte) ([]byte, error) {
	return honghuaOperateExt(ctx, accountID, cmd, int(cmd)+honghuaExtBase, extBody)
}

func honghuaOperateExt(ctx context.Context, accountID string, cmd int64, extField int, extBody []byte) ([]byte, error) {
	b := proto.NewBuilder()
	b.FieldInt64(1, honghuaActivityID)
	b.FieldInt64(2, cmd)
	body := b.Bytes()
	// 扩展块必须带（即使是空子消息），否则服务端一律返回「活动参数错误」。
	// 注意：proto.Builder 的 FieldMessage/FieldBytes 遇到空 slice 会直接跳过，
	// 这里必须手动拼 tag+len，否则空块根本不会写进请求体。
	if extField > 0 {
		body = honghuaAppendMsg(body, extField, extBody)
	}
	return rpcRequest(ctx, accountID, actSvc, "Operate", body, 20*time.Second)
}

// honghuaAppendVarint 追加 varint 编码。
func honghuaAppendVarint(b []byte, v uint64) []byte {
	for {
		x := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b = append(b, x|0x80)
		} else {
			b = append(b, x)
			break
		}
	}
	return b
}

// honghuaAppendMsg 追加 length-delimited 字段（wiretype=2），内容允许为空。
func honghuaAppendMsg(b []byte, field int, sub []byte) []byte {
	b = honghuaAppendVarint(b, uint64(field)<<3|2)
	b = honghuaAppendVarint(b, uint64(len(sub)))
	return append(b, sub...)
}

// ===== 状态：GET /api/activity/honghua =====
// 返回活动配置（时间/档位/奖励）+ 公益金今日已捐标记 + 爱心值/公益金进度（best-effort）。
func handleHonghuaStatus(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	fundClaimed := honghuaFundMarkedToday(accountID)

	// 实时进度：累计已捐赠爱心值 + 全服进度 + 各档位阈值/奖励/可领状态
	donated, serverTotal, serverGoal, tiers, ok := honghuaProgress(ctx, accountID)
	if !ok {
		writeJSONMap(w, "ok", false, "error", "拉取活动进度失败：账号可能不在线或网关繁忙，请稍后重试")
		return
	}
	var serverPercent float64
	if serverGoal > 0 {
		serverPercent = float64(serverTotal) / float64(serverGoal) * 100
	}
	if tiers == nil {
		tiers = []map[string]interface{}{}
	}

	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"data": map[string]interface{}{
			"activityId":   honghuaActivityID,
			"name":         "公益小红花",
			"uid":          "CharityRedFlower",
			"startTime":    1788192000, // 2026-09-01 00:00 北京时间
			"endTime":      1788969599, // 2026-09-09 23:59:59 北京时间
			"seeds":        honghuaSeedName(), // 小红花种子（CDN ItemInfo 20883，同步后动态读取）
			"fruits":       honghuaFruitName(), // 小红花果实（CDN ItemInfo 40883，同步后动态读取）
			"love":         donated, // 累计已捐赠爱心值（领取档位奖励不会减少）
			"serverFund":   serverTotal,
			"serverGoal":   serverGoal,
			"serverPercent": serverPercent,
			"tiers":        tiers, // [{threshold, donated, claimable, itemId, itemName, count}]
			"claimed":      false,
			"fundClaimedToday": fundClaimed,
			"dailyGift": map[string]interface{}{
				"itemId": honghuaRewardItemOrganic, "itemName": "有机化肥(1小时)",
				"count": 2, "note": "活动期间每日收获小红花即可领取，每日限领 1 次",
			},
			"settleGift": map[string]interface{}{
				"note": "全服达成公益目标后满足参与条件可领：化肥礼包×20、金豆豆×200、点券×300，单角色限领 1 次",
			},
			"cmd": map[string]interface{}{
				"love":   honghuaLoveCmd,
				"fund":   honghuaFundCmd,
				"claimDaily": honghuaClaimDailyCmd,
				"claimTier":  honghuaClaimTierCmd,
				"claimSettle": honghuaClaimSettleCmd,
				"note": "cmd=35/36/38/39 均已线上实测确认；扩展块字段号=cmd+99（必带，可空）；cmd=37/40 实测无效",
			},
		},
	})
}

// honghuaLiveProgress best-effort 读 爱心值/公益金 实时进度。
// 捐赠响应：cmd36→f136(7,7,7,2027314 计数)，cmd38→f138(订单)。GetGroup(2026090901) 应含类似进度。
// 字段未确认，解析失败返回 (0,0)。
// honghuaProgress 从 GetGroup(2026090901) 的活动进度块 f116 读取实时数据。
// 字段（抓包实锤，对照 flow225/flow367/flow369）：
//
//	f1 = 1040        爱心值物品 id（不是数量！背包里也是这个 id）
//	f3 = 累计已捐赠爱心值  ← 核心：cmd36 捐 7 点后 f3=7，与 ItemNotify「1040 -7」完全吻合。
//	                       这是累计值，领取档位奖励后不会减少。
//	f4 = 全服累计进度值（单位：分，÷100 = 元）
//	f5 = 全服目标值（1000000000 分 = 1000万元，进度 = f4/f5）
//	f9 = repeated 档位 { f1=阈值(30/60/90/120/150), f2=奖励{ f1=物品id, f2=数量 } }
//
// 注意 f3 只在捐赠过爱心值后出现；从未捐赠时该字段缺失，按 0 处理。
//
// GetGroup 偶发失败（账号连接池忙/超时）时回退最近一次成功结果（30s 内），
// 避免前端因一次抖动看到 0/0 或「加载中…」。无缓存才返回失败。
var (
	hhProgMu    sync.Mutex
	hhProgCache map[string]hhProgEntry
)

type hhProgEntry struct {
	donated, serverTotal, serverGoal int64
	tiers                           []map[string]interface{}
	ts                              time.Time
}

func honghuaProgress(ctx context.Context, accountID string) (donated, serverTotal, serverGoal int64, tiers []map[string]interface{}, ok bool) {
	b := proto.NewBuilder()
	b.FieldInt64(1, honghuaActivityID)
	b.FieldString(2, "")
	body, err := rpcRequest(ctx, accountID, actSvc, "GetGroup", b.Bytes(), 15*time.Second)
	if err != nil {
		return hhProgFallback(accountID)
	}
	// GetGroup 响应里 f116 埋在 body.f1.f1 两层嵌套下，用递归找，避免漏层。
	var find116 func(fs []actField) []byte
	find116 = func(fs []actField) []byte {
		for _, f := range fs {
			if f.No == 116 && f.Wire == 2 && len(f.Bytes) > 0 {
				return f.Bytes
			}
			if f.Wire == 2 && len(f.Bytes) > 0 {
				if sub := find116(readActFields(f.Bytes)); len(sub) > 0 {
					return sub
				}
			}
		}
		return nil
	}
	if raw := find116(readActFields(body)); len(raw) > 0 {
		g := readActFields(raw)
		donated = actNum(g, 3)          // 累计已捐赠爱心值（缺失时为 0）
		serverTotal = actNum(g, 4) / 100 // 全服累计公益金（服务端单位=分，÷100 转元，≈3.8万元）
		serverGoal = actNum(g, 5) / 100  // 全服目标（分→元 = 1000万元，游戏 UI 与之一致；不除以 100 会显示成 10亿）
		for _, tf := range g {
			if tf.No != 9 || tf.Wire != 2 || len(tf.Bytes) == 0 {
				continue
			}
			tfFields := readActFields(tf.Bytes)
			threshold := actNum(tfFields, 1)
			item := map[string]interface{}{
				"threshold": threshold,
				"donated":   donated, // 每档共用同一个累计已捐值
				"claimable": donated >= threshold,
			}
			if rb := actBytes(tfFields, 2); len(rb) > 0 {
				rfs := readActFields(rb)
				if iid := actNum(rfs, 1); iid > 0 {
					item["itemId"] = iid
					item["itemName"] = honghuaItemName(iid)
					item["count"] = actNum(rfs, 2)
				}
			}
			tiers = append(tiers, item)
		}
		// 解析成功 → 写缓存
		hhProgMu.Lock()
		if hhProgCache == nil {
			hhProgCache = map[string]hhProgEntry{}
		}
		hhProgCache[accountID] = hhProgEntry{donated: donated, serverTotal: serverTotal, serverGoal: serverGoal, tiers: tiers, ts: time.Now()}
		hhProgMu.Unlock()
		return donated, serverTotal, serverGoal, tiers, true
	}
	return hhProgFallback(accountID)
}

// hhProgFallback GetGroup 失败或未解析出 f116 时回退最近一次成功缓存。
// 不设时效限制：只要后端成功解析过一次（缓存有值），就返回该数据，避免接口空返回导致前端显示 --/0/0。
func hhProgFallback(accountID string) (donated, serverTotal, serverGoal int64, tiers []map[string]interface{}, ok bool) {
	hhProgMu.Lock()
	defer hhProgMu.Unlock()
	if e, has := hhProgCache[accountID]; has && e.serverGoal > 0 {
		return e.donated, e.serverTotal, e.serverGoal, e.tiers, true
	}
	return 0, 0, 0, nil, false
}

// ===== 送出爱心值：POST /api/activity/honghua/love =====
func handleHonghuaLove(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	body, err := honghuaOperate(ctx, accountID, honghuaLoveCmd, nil)
	if err != nil {
		writeJSONMap(w, "ok", false, "error", actErrMsg(err))
		return
	}
	res := honghuaParseDonateResult(body, honghuaLoveCmd)
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID, "cmd": honghuaLoveCmd,
		"data": res,
	})
}

// ===== 送出公益金：POST /api/activity/honghua/fund =====
// 单账号活动期仅 1 次资格 + 真实 1 元扣款，已捐防重（内存标记）。
func handleHonghuaFund(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	if honghuaFundMarkedToday(accountID) {
		writeJSONMap(w, "ok", true, "account", accountID, "alreadyDonated", true,
			"msg", "今日已送出公益金（单账号活动期仅 1 次资格）")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	body, err := honghuaOperate(ctx, accountID, honghuaFundCmd, nil)
	if err != nil {
		es := actErrMsg(err)
		// 已获得资格 / 已捐赠：标记并返回成功语义
		if strings.Contains(es, "已") && (strings.Contains(es, "捐赠") || strings.Contains(es, "资格") || strings.Contains(es, "公益金")) {
			honghuaFundMark(accountID)
			writeJSONMap(w, "ok", true, "account", accountID, "alreadyDonated", true, "msg", es)
			return
		}
		writeJSONMap(w, "ok", false, "error", es)
		return
	}
	honghuaFundMark(accountID) // 成功送出 → 标记防重
	res := honghuaParseDonateResult(body, honghuaFundCmd)
	// 公益礼包：送出公益金时随订单一并到账的奖励（f138.f3 = 化肥(1小时)×2），
	// 不是独立的领取操作，规则里「每日限领 1 次」由公益金的单次资格自然保证。
	if item, ok := res["item"].(map[string]interface{}); ok {
		res["gift"] = map[string]interface{}{
			"name":     "公益礼包",
			"itemId":   item["id"],
			"itemName": item["name"],
			"count":    item["count"],
		}
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID, "cmd": honghuaFundCmd, "donated": true,
		"data": res,
	})
}

// ===== 领取奖励：POST /api/activity/honghua/claim =====
// kind=tier|daily|settle（默认 tier）；?cmd= 覆盖真实 cmd；?tier= 档位阈值(30/60/90/120/150)。
// 领取 cmd 已线上实测确认（见常量），?cmd 仅用于覆盖试其它值。
func handleHonghuaClaim(w http.ResponseWriter, r *http.Request) {
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeJSONMap(w, "ok", false, "error", "缺少 accountId")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "tier"
	}
	cmd := int64(honghuaClaimTierCmd)
	switch kind {
	case "daily":
		cmd = honghuaClaimDailyCmd
	case "settle":
		cmd = honghuaClaimSettleCmd
	case "share":
		cmd = honghuaClaimShareCmd // 领取分享奖励（实测有效）
	case "tier":
		cmd = honghuaClaimTierCmd
	}
	if v := r.URL.Query().Get("cmd"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			cmd = n
		}
	}
	// daily/settle 的 cmd 至今未实测成功（cmd=37/40 线上实测返回「活动参数错误」）。
	// 与其发出必然失败的请求再笼统报「活动参数错误」，不如直接说清是功能未确认。
	if (kind == "daily" || kind == "settle") && r.URL.Query().Get("cmd") == "" {
		writeJSONMap(w, "ok", false, "kind", kind, "cmd", cmd,
			"error", "该领取项的操作指令尚未确认（cmd="+itoa(cmd)+" 线上实测无效），需抓包拿到真实 cmd 后才能实现")
		return
	}
	// 档位领取：尝试带 tier 阈值参数（字段/结构未确认，best-effort 用 field1=tier；
	// 若服务端不需要参数，空 ext 也能工作；若需要别的结构，线上用 debug 接口探）。
	// 各档位共用 cmd=39，靠扩展块里的阈值参数区分（30/60/90/120/150）。
	var ext []byte
	var tierThreshold int64
	if kind == "tier" {
		if tv := r.URL.Query().Get("tier"); tv != "" {
			if thr, e := strconv.ParseInt(tv, 10, 64); e == nil {
				tierThreshold = thr
				sub := proto.NewBuilder()
				sub.FieldInt64(1, thr)
				ext = sub.Bytes()
			}
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	body, err := honghuaOperate(ctx, accountID, cmd, ext)
	if err != nil {
		es := actErrMsg(err)
		if strings.Contains(es, "已领取") || strings.Contains(es, "已领") || strings.Contains(es, "无可领取") {
			writeJSONMap(w, "ok", true, "account", accountID, "alreadyClaimed", true, "cmd", cmd, "kind", kind, "msg", es)
			return
		}
		writeJSONMap(w, "ok", false, "error", es, "cmd", cmd, "kind", kind)
		return
	}
	// 领取成功 → 清 group 缓存让状态刷新
	actCacheDel(actGroupCacheKey(accountID, honghuaActivityID))
	actCacheDel(actGroupCacheKey(accountID, 2026090900))

	rewards := parseActRewardField(body, 126)
	// 诚实原则：服务端没返回奖励数据，就不能报「领取成功」。
	// 实测 cmd=39 在档位未达成时同样返回成功码，但回包里没有任何奖励字段
	//（f139 只是请求参数的回显，不是奖励），此前无条件报成功属于误导。
	if len(rewards) == 0 {
		// 服务端不通过 GetGroup 暴露个人爱心值/档位领取状态（f116 只有阈值与奖励），
		// 故只能按「回包无奖励」判定未领到，并把最可能的原因说清楚。
		errMsg := "未领取到奖励：服务端未返回奖励数据（条件未达成或已领取）"
		if kind == "tier" && tierThreshold > 0 {
			errMsg = "未领取到奖励：累计爱心值未达到 " + itoa(tierThreshold) + " 点档位，或该档位已领取"
		}
		writeJSON(w, map[string]interface{}{
			"ok": false, "account": accountID,
			"cmd": cmd, "kind": kind, "tier": tierThreshold,
			"rewards": []map[string]interface{}{},
			"error":   errMsg,
		})
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "account": accountID,
		"cmd": cmd, "kind": kind,
		"rewards": rewards,
		"msg": "领取成功",
	})
}

// honghuaParseDonateResult 解析捐赠回包：
// cmd36→f136(7,7,7,2027314 计数器)；cmd38→f138(订单号 + item 80001×2)。
func honghuaParseDonateResult(body []byte, cmd int64) map[string]interface{} {
	out := map[string]interface{}{"cmd": cmd}
	if cmd == honghuaLoveCmd {
		if raw := subFieldBytes(body, 136); len(raw) > 0 {
			fs := readActFields(raw)
			out["loveValue"] = actNum(fs, 4)
			out["counter"] = []int64{actNum(fs, 1), actNum(fs, 2), actNum(fs, 3)}
		}
	} else if cmd == honghuaFundCmd {
		if raw := subFieldBytes(body, 138); len(raw) > 0 {
			fs := readActFields(raw)
			out["orderNo"] = actStr(fs, 2)
			for _, it := range actBytesAll(fs, 3) {
				ifs := readActFields(it)
				out["item"] = map[string]interface{}{
					"id": actNum(ifs, 1), "count": actNum(ifs, 2),
					"name": honghuaItemName(actNum(ifs, 1)),
				}
			}
		}
	}
	return out
}
