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

// SellItem 出售物品（字段对齐 Node warehouse.js toSellItem → corepb.Item{id=1,count=2,uid=6}）
type SellItem struct {
	ID    int64
	Count int64
	UID   int64
}

// EncodeUseRequest 对齐 Node itempb.proto UseRequest{item_id=1,count=2}
func EncodeUseRequest(itemID, count int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, itemID)
	b.FieldInt64(2, count)
	return b.Bytes()
}

// EncodeUseRequestFallback 使用物品的 raw protobuf 回退编码。
//
// 严格照抄 Node core/src/services/warehouse.js useItem() 的 catch 分支（118-134 行）：
//
//	writer.uint32(10).fork();                // field 1, wire 2（嵌套 message）
//	writer.uint32(8).int64(toLong(itemId));  // 内层 field 1, varint
//	writer.uint32(16).int64(toLong(count));  // 内层 field 2, varint
//	writer.ldelim();
//
// 即：外层 field1 是一个 length-delimited 子消息，子消息里才是 item_id/count。
// 与 itempb.proto 里 UseRequest{item_id=1,count=2} 的平铺结构不同——线上服务端
// 实际期望的是这种嵌套形态，proto 文件已过时，故 Node 才需要这个回退。
// 用 Always 版本以忠实对齐 protobuf.js Writer 低层 API（不做默认值跳过）。
func EncodeUseRequestFallback(itemID, count int64) []byte {
	sub := NewBuilder()
	sub.FieldInt64Always(1, itemID)
	sub.FieldInt64Always(2, count)
	b := NewBuilder()
	b.FieldMessage(1, sub.Bytes())
	return b.Bytes()
}

// IsBadParamError 对齐 Node warehouse.js:120 的判定：
// msg.includes('code=1000020') || msg.includes('请求参数错误')
func IsBadParamError(msg string) bool {
	return strings.Contains(msg, "code=1000020") || strings.Contains(msg, "请求参数错误")
}

// EncodeSellRequest 对齐 Node itempb.proto SellRequest{items=1} 每项 corepb.Item{id=1,count=2,uid=6}
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

// EncodeBatchUseRequest 对齐 Node itempb.proto BatchUseRequest{items=1} 每项 corepb.Item{id=1,count=2,uid=6}
// （对齐 Node warehouse.js batchUseItems：id/count/uid 均为 int64，uid 缺省 0）
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

// IsFertilizerContainerFullError 对齐 Node warehouse.js isFertilizerContainerFullError：
// code=1003002 或容器已满文案 → 静默返回（视为"已满，无需填充"）
func IsFertilizerContainerFullError(msg string) bool {
	return strings.Contains(msg, "code=1003002") ||
		strings.Contains(msg, "普通化肥容器已达到上限") ||
		strings.Contains(msg, "普通化肥容器已满") ||
		strings.Contains(msg, "有机化肥容器已达到上限") ||
		strings.Contains(msg, "有机化肥容器已满")
}
