package proto

// DecodeLandsNotifyHostGid 解码土地变化通知的 host_gid（plantpb.proto LandsNotify: lands=1, host_gid=2）
// 仅取 host_gid 用于判断通知是否属于本账号农场；0 或等于自身 GID 即本账号。
func DecodeLandsNotifyHostGid(body []byte) int64 {
	if len(body) == 0 {
		return 0
	}
	r := NewReader(body)
	var host int64
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 2 && wire == WireVarint {
			host = r.ReadInt64()
		}
		return true
	})
	return host
}

// ItemChg 物品变化（notifypb.ItemNotify -> corepb.ItemChg）
type ItemChg struct {
	ID    int64
	Count int64
	Delta int64
}

// 同气连枝礼包物品 ID（帮忙好友概率获得）
const ItemIDTongQiGift = 101351

// DecodeItemNotify 解码物品变化通知（notifypb.ItemNotify）
// 结构: ItemNotify{ repeated ItemChg items = 1 }  ItemChg{ Item item=1; int64 delta=2 }  Item{ int64 id=1; int64 count=2 }
func DecodeItemNotify(body []byte) []ItemChg {
	if len(body) == 0 {
		return nil
	}
	var out []ItemChg
	r := NewReader(body)
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			sub := r.ReadBytes()
			var it ItemChg
			sr := NewReader(sub)
			sr.EachField(func(f2, w2 int, sr *Reader) bool {
				switch f2 {
				case 1: // Item message
					itb := sr.ReadBytes()
					ir := NewReader(itb)
					ir.EachField(func(f3, w3 int, ir *Reader) bool {
						if w3 == WireVarint {
							switch f3 {
							case 1:
								it.ID = ir.ReadInt64()
							case 2:
								it.Count = ir.ReadInt64()
							}
							return true
						}
						ir.Skip(w3)
						return true
					})
				case 2:
					it.Delta = sr.ReadInt64()
				}
				return true
			})
			out = append(out, it)
		}
		return true
	})
	return out
}
