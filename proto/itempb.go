package proto

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
