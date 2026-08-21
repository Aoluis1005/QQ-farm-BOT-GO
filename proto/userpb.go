package proto

// gamepb.userpb 登录 / 心跳 编解码
// 对应 proto/userpb.proto

// LoginRequest 登录请求
func EncodeLoginRequest(clientVersion string) []byte {
	b := NewBuilder()
	b.FieldInt64(3, 0) // sharer_id
	b.FieldString(4, "") // sharer_open_id

	// device_info field=5
	di := NewBuilder()
	di.FieldString(1, clientVersion)
	di.FieldString(2, "iOS 26.2.1")
	di.FieldString(3, "")    // sys_hardware
	di.FieldString(4, "")    // telecom_oper
	di.FieldString(5, "wifi")
	di.FieldInt64(10, 7672) // memory
	di.FieldString(13, "iPhone X<iPhone18,3>")
	b.FieldMessage(5, di.Bytes())

	b.FieldInt64(6, 0)            // share_cfg_id
	b.FieldString(7, "1256")      // scene_id

	// report_data field=8
	rd := NewBuilder()
	rd.FieldString(5, "other") // minigame_channel
	rd.FieldInt32(6, 2)        // minigame_platid
	b.FieldMessage(8, rd.Bytes())

	return b.Bytes()
}

// BasicInfo 登录返回的基础信息
type BasicInfo struct {
	GID   int64
	Name  string
	Level int64
	Exp   int64
	Gold  int64
	OpenID string
	Avatar string
}

// LoginReply 登录回复
type LoginReply struct {
	Basic         *BasicInfo
	TimeNowMillis int64
}

// DecodeLoginReply 解析登录回复
func DecodeLoginReply(buf []byte) *LoginReply {
	r := NewReader(buf)
	rep := &LoginReply{}
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			if wire == WireLen {
				rep.Basic = decodeBasicInfo(r.ReadBytes())
			} else {
				r.Skip(wire)
			}
		case 3:
			rep.TimeNowMillis = r.ReadInt64()
		default:
			r.Skip(wire)
		}
		return true
	})
	return rep
}

// decodeBasicInfo 解析 BasicInfo 子消息（登录 Basic 与 BasicNotify.basic 复用同字段布局）。
func decodeBasicInfo(sub []byte) *BasicInfo {
	bi := &BasicInfo{}
	er := NewReader(sub)
	er.EachField(func(f, w int, r *Reader) bool {
		switch f {
		case 1:
			bi.GID = r.ReadInt64()
		case 2:
			bi.Name = r.ReadString()
		case 3:
			bi.Level = r.ReadInt64()
		case 4:
			bi.Exp = r.ReadInt64()
		case 5:
			bi.Gold = r.ReadInt64()
		case 6:
			bi.OpenID = r.ReadString()
		case 7:
			bi.Avatar = r.ReadString()
		default:
			r.Skip(w)
		}
		return true
	})
	return bi
}

// DecodeBasicNotify 解析基础信息变化通知（BasicNotify，field1=basis/BasicInfo）。
// 服务端会定期推送绝对余额/经验/等级，用于把被 ItemNotify 变化量污染的余额校准回真实值
func DecodeBasicNotify(buf []byte) *BasicInfo {
	var bi *BasicInfo
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			bi = decodeBasicInfo(r.ReadBytes())
		} else {
			r.Skip(wire)
		}
		return true
	})
	return bi
}

// EncodeHeartbeatRequest 心跳请求
func EncodeHeartbeatRequest(gid int64, clientVersion string) []byte {
	b := NewBuilder()
	b.FieldInt64Always(1, gid)
	b.FieldString(2, clientVersion)
	return b.Bytes()
}

// EncodeReportArkClickRequest 主动加好友（分享卡 → UserService.ReportArkClick）。
// f1 sharer_id, f2 sharer_open_id,
// f3 share_cfg_id=string"1008", f4 scene_id=7, f5 share_key。
func EncodeReportArkClickRequest(uid int64, openId, shareKey string) []byte {
	b := NewBuilder()
	b.FieldInt64(1, uid) // sharer_id
	b.FieldString(2, openId)
	b.FieldString(3, "1008") // share_cfg_id 固定字符串 "1008"
	b.FieldInt64(4, 7)       // scene_id 固定 7
	b.FieldString(5, shareKey)
	return b.Bytes()
}
