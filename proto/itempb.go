package proto

import "strings"

// gamepb.itempb / corepb 背包编解码

// 点券/金豆物品ID
const (
	ItemIDCoupon   = 1002 // 点券
	ItemIDGoldBean = 1005 // 金豆豆
)

// BagItem 背包物品
type BagItem struct {
	ID    int64
	Count int64
	UID   int64 // 物品实例 uid（corepb.Item.uid=6），出售时需回传
}

// BagReply 背包响应
type BagReply struct {
	Items []BagItem
}

// EncodeBagRequest 空请求体
func EncodeBagRequest() []byte { return nil }

// DecodeBagReply 解析背包：BagReply{item_bag=1} -> ItemBag{items=1} -> Item{id=1,count=2}
func DecodeBagReply(buf []byte) *BagReply {
	rep := &BagReply{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			itemBag := r.ReadBytes()
			rb := NewReader(itemBag)
			rb.EachField(func(f2, w2 int, r2 *Reader) bool {
				if f2 == 1 && w2 == WireLen {
					itemRaw := r2.ReadBytes()
					st := NewReader(itemRaw)
					var it BagItem
					st.EachField(func(f3, w3 int, r3 *Reader) bool {
						switch f3 {
						case 1:
							it.ID = r3.ReadInt64()
						case 2:
							it.Count = r3.ReadInt64()
						case 6:
							it.UID = r3.ReadInt64()
						default:
							r3.Skip(w3)
						}
						return true
					})
					rep.Items = append(rep.Items, it)
				} else {
					r2.Skip(w2)
				}
				return true
			})
		} else {
			r.Skip(wire)
		}
		return true
	})
	return rep
}

// SellItem 出售物品（字段,count=2,uid=6}）
type SellItem struct {
	ID    int64
	Count int64
	UID   int64
}

// EncodeUseRequest ,count=2}
func EncodeUseRequest(itemID, count int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, itemID)
	b.FieldInt64(2, count)
	return b.Bytes()
}

// EncodeUseRequestFallback 使用物品的 raw protobuf 回退编码。
// 严格照抄 Node core/src/services/warehouse.js useItem() 的 catch 分支（118-134 行）：
//	writer.uint32(10).fork();                // field 1, wire 2（嵌套 message）
//	writer.uint32(8).int64(toLong(itemId));  // 内层 field 1, varint
//	writer.uint32(16).int64(toLong(count));  // 内层 field 2, varint
//	writer.ldelim();
// 即：外层 field1 是一个 length-delimited 子消息，子消息里才是 item_id/count。
// 与 itempb.proto 里 UseRequest{item_id=1,count=2} 的平铺结构不同——线上服务端
// 实际期望的是这种嵌套形态，proto 文件已过时，故 Node 才需要这个回退。
// 用 Always 版本以忠实（不做默认值跳过）。
func EncodeUseRequestFallback(itemID, count int64) []byte {
	sub := NewBuilder()
	sub.FieldInt64Always(1, itemID)
	sub.FieldInt64Always(2, count)
	b := NewBuilder()
	b.FieldMessage(1, sub.Bytes())
	return b.Bytes()
}

// EncodeUseRequestWithLands ,count=2,land_ids=3}，
// 用于「在土地上使用物品」类操作（如鹊桥灵露喷洒：item_id=301103，land_ids=[目标地块]）。
// 在 EncodeUseRequest 的平铺结构基础上补上重复 int64 的 land_ids（field 3）。
func EncodeUseRequestWithLands(itemID, count int64, landIDs []int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, itemID)
	b.FieldInt64(2, count)
	for _, lid := range landIDs {
		b.FieldInt64(3, lid)
	}
	return b.Bytes()
}

// IsBadParamError 120 的判定：
// msg.includes('code=1000020') || msg.includes('请求参数错误')
func IsBadParamError(msg string) bool {
	return strings.Contains(msg, "code=1000020") || strings.Contains(msg, "请求参数错误")
}

// EncodeSellRequest ,count=2,uid=6}
func EncodeSellRequest(items []SellItem) []byte {
	b := NewBuilder()
	for _, it := range items {
		sub := NewBuilder()
		sub.FieldInt64(1, it.ID)
		sub.FieldInt64(2, it.Count)
		sub.FieldInt64(6, it.UID)
		b.FieldMessage(1, sub.Bytes())
	}
	return b.Bytes()
}

// EncodeBatchUseRequest ,count=2,uid=6}
func EncodeBatchUseRequest(items []SellItem) []byte {
	b := NewBuilder()
	for _, it := range items {
		sub := NewBuilder()
		sub.FieldInt64(1, it.ID)
		sub.FieldInt64(2, it.Count)
		sub.FieldInt64(6, it.UID)
		b.FieldMessage(1, sub.Bytes())
	}
	return b.Bytes()
}

// IsFertilizerContainerFullError 
// code=1003002 或容器已满文案 → 静默返回（视为"已满，无需填充"）
func IsFertilizerContainerFullError(msg string) bool {
	return strings.Contains(msg, "code=1003002") ||
		strings.Contains(msg, "普通化肥容器已达到上限") ||
		strings.Contains(msg, "普通化肥容器已满") ||
		strings.Contains(msg, "有机化肥容器已达到上限") ||
		strings.Contains(msg, "有机化肥容器已满")
}

// DecodeSellReply 解析 SellReply，返回(出售物品总件数, 获得金币数)
// SellReply sell_items=1 / get_items=2，金币 item id==1 或 1001。
// 注意：不认 500001——真实 Sell 响应 get_items 里的 500001 条目 count 是金币余额，
// 权威 getGoldFromItems 只认 id==1||1001；认 500001 会把余额整额当成收益（导致收益显示 100 多亿）。
func DecodeSellReply(buf []byte) (soldCount, gold int64) {
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if wire != WireLen {
			r.Skip(wire)
			return true
		}
		itemRaw := r.ReadBytes()
		st := NewReader(itemRaw)
		var it SellItem
		st.EachField(func(f3, w3 int, r3 *Reader) bool {
			switch f3 {
			case 1:
				it.ID = r3.ReadInt64()
			case 2:
				it.Count = r3.ReadInt64()
			case 6:
				it.UID = r3.ReadInt64()
			default:
				r3.Skip(w3)
			}
			return true
		})
		if field == 1 { // sell_items
			soldCount += it.Count
		} else if field == 2 { // get_items：金币 id==1 或 1001
			if it.ID == 1001 || it.ID == 1 {
				gold += it.Count
			}
		}
		return true
	})
	return soldCount, gold
}
