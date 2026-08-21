package proto

// gamepb.illustratedpb 图鉴编解码

// IllustratedItem 图鉴条目
type IllustratedItem struct {
	SeedID       int64 // 图鉴返回的果实/条目ID（Node 按 seed_id 读取后当作 fruitId 处理）
	IllustratedTier int32
	Unlocked     bool
	RewardScore  int32
	HarvestCount int32
	HasReward    bool
}

// IllustratedListReply 图鉴列表回复
type IllustratedListReply struct {
	Items []IllustratedItem
}

// EncodeGetIllustratedListV2Request ,illustrated_type=2}
func EncodeGetIllustratedListV2Request(refresh bool, illustratedType int) []byte {
	b := NewBuilder()
	b.FieldBool(1, refresh)
	b.FieldInt32(2, int32(illustratedType))
	return b.Bytes()
}

// DecodeGetIllustratedListV2Reply 解析 GetIllustratedListV2Reply{items=1}
// IllustratedItem{seed_id=1, illustrated_tier=2, unlocked=3, reward_score=4, harvest_count=5, reward_info=6(bytes), has_reward=7}
func DecodeGetIllustratedListV2Reply(buf []byte) *IllustratedListReply {
	rep := &IllustratedListReply{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			itemRaw := r.ReadBytes()
			ri := NewReader(itemRaw)
			var it IllustratedItem
			ri.EachField(func(f2, w2 int, r2 *Reader) bool {
				switch f2 {
				case 1:
					it.SeedID = r2.ReadInt64()
				case 2:
					it.IllustratedTier = int32(r2.ReadInt64())
				case 3:
					it.Unlocked = r2.ReadInt64() != 0
				case 4:
					it.RewardScore = int32(r2.ReadInt64())
				case 5:
					it.HarvestCount = int32(r2.ReadInt64())
				case 6:
					r2.Skip(w2) // reward_info bytes：本需求未用
				case 7:
					it.HasReward = r2.ReadInt64() != 0
				default:
					r2.Skip(w2)
				}
				return true
			})
			if it.SeedID > 0 {
				rep.Items = append(rep.Items, it)
			}
		} else {
			r.Skip(wire)
		}
		return true
	})
	return rep
}

// ClaimAllRewardsV2 领取全部已达标图鉴奖励
// ClaimAllRewardsV2Request: only_claimable=1
func EncodeClaimAllRewardsV2Request(onlyClaimable bool) []byte {
	b := NewBuilder()
	b.FieldBool(1, onlyClaimable)
	return b.Bytes()
}

// ClaimAllRewardsV2Reply: items=1, bonus_items=4（仅统计奖品数量用）
// 警告：不要用返回值判断"是否真的领到奖励"。即使没有可领奖励，服务端仍会返回带
// items/bonus_items 的响应，据此判定会导致任务循环每轮都误判成功并写日志（日志刷屏）。
// 判断是否真实到账请用背包点券余额差，见 task_auto.go claimIllustratedRewardsGo。
func DecodeClaimAllRewardsV2Reply(buf []byte) (itemCount int) {
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1, 4:
			// repeated corepb.Item：逐条统计
			if wire == WireLen {
				r.Skip(wire)
				itemCount++
			}
		default:
			r.Skip(wire)
		}
		return true
	})
	return
}
