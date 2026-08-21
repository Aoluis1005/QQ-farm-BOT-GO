package proto

// gamepb.shoppb 商店编解码

// GoodsCond 商品购买条件
type GoodsCond struct {
	Type  int64
	Param int64
}

// GoodsInfo 商品信息
type GoodsInfo struct {
	ID         int64 // 商品ID（购买用）
	BoughtNum  int64 // 已购买数量
	Price      int64 // 价格（金币/金豆豆）
	LimitCount int64 // 限购数量（0=不限）
	Unlocked   bool  // 是否已解锁
	ItemID     int64 // 物品ID（即种子ID）
	Count      int64 // 每次购买获得数量
	Conds      []GoodsCond
}

// ShopInfoReply 商店商品回复
type ShopInfoReply struct {
	GoodsList []GoodsInfo
}

// EncodeShopInfoRequest 
func EncodeShopInfoRequest(shopID int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, shopID)
	return b.Bytes()
}

// DecodeShopInfoReply 解析 ShopInfoReply{goods_list=1} -> GoodsInfo
// GoodsInfo{id=1,bought_num=2,price=3,limit_count=4,unlocked=5,item_id=6,item_count=7,conds=8}
func DecodeShopInfoReply(buf []byte) *ShopInfoReply {
	rep := &ShopInfoReply{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			goodsRaw := r.ReadBytes()
			rep.GoodsList = append(rep.GoodsList, decodeGoodsInfo(goodsRaw))
		} else {
			r.Skip(wire)
		}
		return true
	})
	return rep
}

// decodeGoodsInfo 解析单个 GoodsInfo
func decodeGoodsInfo(buf []byte) GoodsInfo {
	var g GoodsInfo
	r := NewReader(buf)
	r.EachField(func(f2, w2 int, r2 *Reader) bool {
		switch f2 {
		case 1:
			g.ID = r2.ReadInt64()
		case 2:
			g.BoughtNum = r2.ReadInt64()
		case 3:
			g.Price = r2.ReadInt64()
		case 4:
			g.LimitCount = r2.ReadInt64()
		case 5:
			g.Unlocked = r2.ReadInt64() != 0
		case 6:
			g.ItemID = r2.ReadInt64()
		case 7:
			g.Count = r2.ReadInt64()
		case 8:
			condRaw := r2.ReadBytes()
			cr := NewReader(condRaw)
			var c GoodsCond
			cr.EachField(func(f3, w3 int, r3 *Reader) bool {
				switch f3 {
				case 1:
					c.Type = r3.ReadInt64()
				case 2:
					c.Param = r3.ReadInt64()
				default:
					r3.Skip(w3)
				}
				return true
			})
			g.Conds = append(g.Conds, c)
		default:
			r2.Skip(w2)
		}
		return true
	})
	return g
}

// BuyGoodsReply 购买商品回复
type BuyGoodsReply struct {
	Goods    GoodsInfo
	GetItems []ItemChange
	CostItems []ItemChange
}

// ItemChange 购买后的获得/消耗物品变更
type ItemChange struct {
	ID    int64
	Count int64
}

// DecodeBuyGoodsReply 解析 BuyGoodsReply{goods=1,get_items=2,cost_items=3}
func DecodeBuyGoodsReply(buf []byte) *BuyGoodsReply {
	rep := &BuyGoodsReply{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			rep.Goods = decodeGoodsInfo(r.ReadBytes())
		case 2, 3:
			ir := NewReader(r.ReadBytes())
			var it ItemChange
			ir.EachField(func(f2, w2 int, r2 *Reader) bool {
				switch f2 {
				case 1:
					it.ID = r2.ReadInt64()
				case 2:
					it.Count = r2.ReadInt64()
				default:
					r2.Skip(w2)
				}
				return true
			})
			if field == 2 {
				rep.GetItems = append(rep.GetItems, it)
			} else {
				rep.CostItems = append(rep.CostItems, it)
			}
		default:
			r.Skip(wire)
		}
		return true
	})
	return rep
}

// EncodeBuyGoodsRequest ,num=2,price=3}
func EncodeBuyGoodsRequest(goodsID, num, price int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, goodsID)
	b.FieldInt64(2, num)
	b.FieldInt64(3, price)
	return b.Bytes()
}
