package proto

// gamepb.shoppb 商店编解码（对齐 Node core/src/proto/shoppb.proto）

// GoodsInfo 商品信息（对齐 Node GoodsInfo）
type GoodsInfo struct {
	ID       int64 // 商品ID（购买用）
	Price    int64 // 价格（金币）
	Unlocked bool  // 是否已解锁
	ItemID   int64 // 物品ID（即种子ID）
	Count    int64 // 每次购买获得数量
}

// ShopInfoReply 商店商品回复
type ShopInfoReply struct {
	GoodsList []GoodsInfo
}

// EncodeShopInfoRequest 对齐 Node ShopInfoRequest{shop_id=1}
func EncodeShopInfoRequest(shopID int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, shopID)
	return b.Bytes()
}

// DecodeShopInfoReply 解析 ShopInfoReply{goods_list=1} -> GoodsInfo{id=1,price=3,unlocked=5,item_id=6,item_count=7}
func DecodeShopInfoReply(buf []byte) *ShopInfoReply {
	rep := &ShopInfoReply{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			goodsRaw := r.ReadBytes()
			rg := NewReader(goodsRaw)
			var g GoodsInfo
			rg.EachField(func(f2, w2 int, r2 *Reader) bool {
				switch f2 {
				case 1:
					g.ID = r2.ReadInt64()
				case 3:
					g.Price = r2.ReadInt64()
				case 5:
					g.Unlocked = r2.ReadInt64() != 0
				case 6:
					g.ItemID = r2.ReadInt64()
				case 7:
					g.Count = r2.ReadInt64()
				default:
					r2.Skip(w2)
				}
				return true
			})
			if g.ID > 0 {
				rep.GoodsList = append(rep.GoodsList, g)
			}
		} else {
			r.Skip(wire)
		}
		return true
	})
	return rep
}

// EncodeBuyGoodsRequest 对齐 Node BuyGoodsRequest{goods_id=1,num=2,price=3}
func EncodeBuyGoodsRequest(goodsID, num, price int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, goodsID)
	b.FieldInt64(2, num)
	b.FieldInt64(3, price)
	return b.Bytes()
}
