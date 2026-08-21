package proto

// gamepb.qqvippb QQ会员编解码
//	GetQQVipRewardsStatusRequest {}
//	QQVipRewardStatus { bool enabled=1; bytes rewards=2; int64 vip_level=3; int64 activity_id=4;
//	                    int32 reward_type=5; bool can_claim=6; int64 begin_time=7; int64 end_time=8 }
//	GetQQVipRewardsStatusReply { bool is_qq_vip=1; bool is_super_vip=2;
//	                             repeated QQVipRewardStatus reward_statuses=5 }
//	RefreshVipInfoRequest {} / RefreshVipInfoReply {}
//	ClaimQQVipRewardsRequest { repeated int32 reward_types=1 [packed = true] }
//	ClaimQQVipRewardsReply { repeated corepb.Item items=3 }

// EncodeEmptyRequest 空请求体（无字段的请求）
func EncodeEmptyRequest() []byte {
	return NewBuilder().Bytes()
}

// EncodeClaimQQVipRewardsRequest 领取会员奖励（reward_types 为 packed repeated int32）
func EncodeClaimQQVipRewardsRequest(rewardTypes []int64) []byte {
	b := NewBuilder()
	if len(rewardTypes) > 0 {
		var inner []byte
		for _, rt := range rewardTypes {
			inner = appendVarintRaw(inner, uint64(rt))
		}
		b.FieldBytes(1, inner) // packed repeated int32
	}
	return b.Bytes()
}

// appendVarintRaw 追加一个 varint 到字节切片（packed 字段内部使用）
func appendVarintRaw(buf []byte, x uint64) []byte {
	for x >= 0x80 {
		buf = append(buf, byte(x)|0x80)
		x >>= 7
	}
	return append(buf, byte(x))
}
