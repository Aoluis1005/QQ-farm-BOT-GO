package proto

// gamepb.emailpb 邮件编解码（对齐 Node core/src/proto/emailpb.proto）
//
//	GetEmailListRequest { int32 box_type=1 }
//	EmailItem { string id=1; int32 mail_type=2; string title=3; bool claimed=4;
//	            bool has_reward=5; int64 sent_at=6; string subtitle=7; int64 status_time=8 }
//	GetEmailListReply { repeated EmailItem emails=1 }
//	BatchClaimEmailRequest { int32 box_type=1; repeated string email_ids=2 }
//	BatchClaimEmailReply {}
//	ClaimEmailRequest { int32 box_type=1; string email_id=2 }
//	ClaimEmailReply { repeated corepb.Item items=1 }

// EncodeGetEmailListRequest 查询邮箱列表（box_type: 1/2）
func EncodeGetEmailListRequest(boxType int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, boxType)
	return b.Bytes()
}

// EncodeBatchClaimEmailRequest 批量领取邮件奖励（box_type + 邮件 id 列表）
func EncodeBatchClaimEmailRequest(boxType int64, emailIDs []string) []byte {
	b := NewBuilder()
	b.FieldInt64(1, boxType)
	for _, id := range emailIDs {
		b.FieldString(2, id)
	}
	return b.Bytes()
}

// EncodeClaimEmailRequest 单条领取邮件奖励（box_type + 邮件 id）
func EncodeClaimEmailRequest(boxType int64, emailID string) []byte {
	b := NewBuilder()
	b.FieldInt64(1, boxType)
	b.FieldString(2, emailID)
	return b.Bytes()
}
