package proto

// gamepb.mallpb 道具商城编解码（对齐 Node core/src/proto/mallpb.proto）
//
//	GetMallListBySlotTypeRequest { int32 slot_type=1 }
//	GetMallListBySlotTypeResponse { repeated bytes goods_list=1; int64 timestamp=2 }
//	MallGoods { int32 goods_id=1; string name=2; int32 type=3; bytes item_ids=4;
//	            bytes price=5; bool is_free=6; bytes limit=7; bool is_limited=8; string discount=10; }
//	PurchaseRequest { int32 goods_id=1; int32 count=2; }
//	PurchaseResponse { int32 goods_id=1; int32 count=2; bytes reward_info=3; bytes result=5; }
//
// 注意：goods_list 是 repeated bytes（每个元素是一个序列化的 MallGoods）；price/limit/item_ids 也是 bytes。

// MallGoods 道具商城商品（对齐 Node MallGoods）
type MallGoods struct {
	GoodsID     int64
	Name        string
	Type        int64
	PriceBytes  []byte // field5，为序列化整数，需 ParseMallPriceValue
	IsFree      bool
	LimitBytes  []byte // field7，序列化 limitCount/boughtNum
	IsLimited   bool
	Discount    string
}

// MallListReply GetMallListBySlotTypeResponse
type MallListReply struct {
	GoodsList []MallGoods
}

// EncodeGetMallListBySlotTypeRequest 对齐 Node GetMallListBySlotTypeRequest{slot_type=1}
func EncodeGetMallListBySlotTypeRequest(slotType int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, slotType) // int32，varint 兼容
	return b.Bytes()
}

// DecodeMallListBySlotTypeReply 解析 goods_list（repeated bytes）为 MallGoods 列表
func DecodeMallListBySlotTypeReply(buf []byte) *MallListReply {
	rep := &MallListReply{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			rep.GoodsList = append(rep.GoodsList, decodeMallGoods(r.ReadBytes()))
		} else {
			r.Skip(wire)
		}
		return true
	})
	return rep
}

// decodeMallGoods 解析单个 MallGoods
func decodeMallGoods(buf []byte) MallGoods {
	var g MallGoods
	r := NewReader(buf)
	r.EachField(func(f, w int, r *Reader) bool {
		switch f {
		case 1:
			g.GoodsID = r.ReadInt64()
		case 2:
			g.Name = r.ReadString()
		case 3:
			g.Type = r.ReadInt64()
		case 4:
			r.Skip(w) // item_ids 序列化整数列表，商城 Tab 未用（Node 未填充）
		case 5:
			g.PriceBytes = r.ReadBytes()
		case 6:
			g.IsFree = r.ReadInt64() != 0
		case 7:
			g.LimitBytes = r.ReadBytes()
		case 8:
			g.IsLimited = r.ReadInt64() != 0
		case 10:
			g.Discount = r.ReadString()
		default:
			r.Skip(w)
		}
		return true
	})
	return g
}

// ParseMallPriceValue 解析 price bytes 中 field1 的 varint 值（对齐 Node parseMallPriceValue）
func ParseMallPriceValue(buf []byte) int64 {
	if len(buf) == 0 {
		return 0
	}
	r := NewReader(buf)
	var v int64
	r.EachField(func(f, w int, r *Reader) bool {
		if f == 1 {
			v = r.ReadInt64()
			return false
		}
		r.Skip(w)
		return true
	})
	return v
}

// ParseMallLimitInfo 解析 limit bytes：field1=limitCount, field2=boughtNum（对齐 Node parseMallLimitInfo）
func ParseMallLimitInfo(buf []byte) (limitCount, boughtNum int64) {
	if len(buf) == 0 {
		return 0, 0
	}
	r := NewReader(buf)
	r.EachField(func(f, w int, r *Reader) bool {
		switch f {
		case 1:
			limitCount = r.ReadInt64()
		case 2:
			boughtNum = r.ReadInt64()
		default:
			r.Skip(w)
		}
		return true
	})
	return
}

// EncodePurchaseRequest 对齐 Node PurchaseRequest{goods_id=1,count=2}（均 int32）
func EncodePurchaseRequest(goodsID, count int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, goodsID)
	b.FieldInt64(2, count)
	return b.Bytes()
}

// PurchaseReply PurchaseResponse
type PurchaseReply struct {
	GoodsID int64
	Count   int64
}

// DecodePurchaseReply 解析 PurchaseResponse{goods_id=1,count=2}
func DecodePurchaseReply(buf []byte) *PurchaseReply {
	rep := &PurchaseReply{}
	r := NewReader(buf)
	r.EachField(func(f, w int, r *Reader) bool {
		switch f {
		case 1:
			rep.GoodsID = r.ReadInt64()
		case 2:
			rep.Count = r.ReadInt64()
		default:
			r.Skip(w)
		}
		return true
	})
	return rep
}
