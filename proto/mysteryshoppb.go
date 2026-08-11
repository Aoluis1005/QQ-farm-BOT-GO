package proto

// gamepb.mysteryshoppb 神秘商人编解码（对齐 Node core/src/proto/mysteryshoppb.proto）
//
//	service MysteryShopService
//	GetActiveNPCRequest {}
//	GetActiveNPCReply { bool active=1; MysteryShopNPC npc=2; int64 start_time=3; int64 end_time=4; }
//	MysteryShopNPC { int64 npc_id=1; int32 item_id=2; int32 item_type=3; int32 item_count=4;
//	                 int32 currency_id=5; int64 price=6; int32 discount=7; bool purchased=8; int64 original_price=9; }
//	BuyRequest { int64 npc_id=1; }
//	BuyReply { RewardInfo reward=1; MysteryShopNPC npc=2; }
//	RewardInfo { int32 item_id=1; int32 count=2; }
//	AbandonRequest {} / AbandonReply {}

// MysteryNPC 神秘商人商品（对齐 Node MysteryShopNPC）
type MysteryNPC struct {
	NpcID         int64
	ItemID        int64
	ItemType      int64
	ItemCount     int64
	CurrencyID    int64
	Price         int64
	Discount      int64
	Purchased     bool
	OriginalPrice int64
}

// GetActiveNPCReply 对齐 Node GetActiveNPCReply
type GetActiveNPCReply struct {
	Active    bool
	NPC       *MysteryNPC
	StartTime int64 // 秒级时间戳
	EndTime   int64 // 秒级时间戳
}

// EncodeGetActiveNPCRequest 空请求
func EncodeGetActiveNPCRequest() []byte { return []byte{} }

// decodeMysteryNPC 解析单个 MysteryShopNPC
func decodeMysteryNPC(buf []byte) *MysteryNPC {
	npc := &MysteryNPC{}
	r := NewReader(buf)
	r.EachField(func(f, w int, r *Reader) bool {
		switch f {
		case 1:
			npc.NpcID = r.ReadInt64()
		case 2:
			npc.ItemID = r.ReadInt64()
		case 3:
			npc.ItemType = r.ReadInt64()
		case 4:
			npc.ItemCount = r.ReadInt64()
		case 5:
			npc.CurrencyID = r.ReadInt64()
		case 6:
			npc.Price = r.ReadInt64()
		case 7:
			npc.Discount = r.ReadInt64()
		case 8:
			npc.Purchased = r.ReadInt64() != 0
		case 9:
			npc.OriginalPrice = r.ReadInt64()
		default:
			r.Skip(w)
		}
		return true
	})
	return npc
}

// DecodeGetActiveNPCReply 解析 GetActiveNPCReply
func DecodeGetActiveNPCReply(buf []byte) *GetActiveNPCReply {
	rep := &GetActiveNPCReply{}
	r := NewReader(buf)
	r.EachField(func(f, w int, r *Reader) bool {
		switch f {
		case 1:
			rep.Active = r.ReadInt64() != 0
		case 2:
			rep.NPC = decodeMysteryNPC(r.ReadBytes())
		case 3:
			rep.StartTime = r.ReadInt64()
		case 4:
			rep.EndTime = r.ReadInt64()
		default:
			r.Skip(w)
		}
		return true
	})
	return rep
}

// EncodeMysteryBuyRequest 对齐 Node BuyRequest{npc_id=1}
func EncodeMysteryBuyRequest(npcID int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, npcID)
	return b.Bytes()
}

// MysteryReward 购买奖励（对齐 Node RewardInfo{item_id=1,count=2}）
type MysteryReward struct {
	ItemID int64
	Count  int64
}

// MysteryBuyReply 对齐 Node BuyReply{reward=1,npc=2}
type MysteryBuyReply struct {
	Reward *MysteryReward
	NPC    *MysteryNPC
}

// DecodeMysteryBuyReply 解析 BuyReply
func DecodeMysteryBuyReply(buf []byte) *MysteryBuyReply {
	rep := &MysteryBuyReply{}
	r := NewReader(buf)
	r.EachField(func(f, w int, r *Reader) bool {
		switch f {
		case 1:
			raw := r.ReadBytes()
			rr := NewReader(raw)
			reward := &MysteryReward{}
			rr.EachField(func(f2, w2 int, r2 *Reader) bool {
				switch f2 {
				case 1:
					reward.ItemID = r2.ReadInt64()
				case 2:
					reward.Count = r2.ReadInt64()
				default:
					r2.Skip(w2)
				}
				return true
			})
			rep.Reward = reward
		case 2:
			rep.NPC = decodeMysteryNPC(r.ReadBytes())
		default:
			r.Skip(w)
		}
		return true
	})
	return rep
}

// EncodeMysteryAbandonRequest 空请求（对齐 Node AbandonRequest{}）
func EncodeMysteryAbandonRequest() []byte { return []byte{} }

// DecodeMysteryAbandonReply AbandonReply{}（空，成功即无错误）
func DecodeMysteryAbandonReply(buf []byte) bool { return len(buf) >= 0 }
