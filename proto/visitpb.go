package proto

// gamepb.visitpb 访问好友农场编解码。
// 好友详情 / 好友地块 / 好友操作前均需 Enter 好友农场获取 lands 与护主犬摘要。

// 护主犬 ID → 名称
var DogNames = map[int64]string{
	90001: "田园犬",
	90002: "牧羊犬",
	90003: "斑点狗",
	90011: "柯基",
	90021: "护主犬",
}

// EncodeVisitEnterRequest 进入好友农场
func EncodeVisitEnterRequest(hostGID int64, reason int64, visitToken string) []byte {
	b := NewBuilder()
	b.FieldInt64(1, hostGID) // host_gid
	if reason != 0 {
		b.FieldInt64(2, reason) // reason：默认 2=偷菜访问；5=主动加好友
	}
	b.FieldString(7, visitToken) // visit_token（加好友流程用，平时空）
	return b.Bytes()
}

// EncodeVisitLeaveRequest 离开好友农场
func EncodeVisitLeaveRequest(hostGID int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, hostGID)
	return b.Bytes()
}

// VisitBasic 好友基本信息（EnterReply.basic，userpb.BasicInfo 关键字段）
type VisitBasic struct {
	GID       int64
	Name      string
	Level     int64
	Gold      int64
	AvatarURL string
}

func decodeVisitBasic(buf []byte) *VisitBasic {
	bi := &VisitBasic{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			bi.GID = r.ReadInt64()
		case 2:
			bi.Name = r.ReadString()
		case 3:
			bi.Level = r.ReadInt64()
		case 5:
			bi.Gold = r.ReadInt64()
		case 7:
			bi.AvatarURL = r.ReadString()
		default:
			r.Skip(wire)
		}
		return true
	})
	return bi
}

// VisitEnterReply 进入好友农场响应
type VisitEnterReply struct {
	Basic        *VisitBasic
	Lands        []*LandInfo
	DogID        int64 // 解析自 brief_dog_info
	DogName      string
}

// DecodeVisitEnterReply 解析 Enter 响应。
// EnterReply: basic=1, lands=2(repeated), brief_dog_info=3, nudge_info=4
func DecodeVisitEnterReply(buf []byte) *VisitEnterReply {
	rep := &VisitEnterReply{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			if wire == WireLen {
				rep.Basic = decodeVisitBasic(r.ReadBytes())
			} else {
				r.Skip(wire)
			}
		case 2:
			if wire == WireLen {
				l := &LandInfo{}
				l.decode(r.ReadBytes())
				rep.Lands = append(rep.Lands, l)
			} else {
				r.Skip(wire)
			}
		case 3:
			if wire == WireLen {
				rep.DogID, rep.DogName = parseBriefDogInfo(r.ReadBytes())
			} else {
				r.Skip(wire)
			}
		default:
			r.Skip(wire)
		}
		return true
	})
	return rep
}

// parseBriefDogInfo 解析 brief_dog_info：收集所有正 varint，
// 命中 DOG_NAMES 里任一 ID 即为护主犬。
func parseBriefDogInfo(buf []byte) (int64, string) {
	varints := collectVarints(buf)
	for _, v := range varints {
		if v <= 0 {
			continue
		}
		if name, ok := DogNames[v]; ok {
			return v, name
		}
	}
	return 0, ""
}

// collectVarints 递归收集消息体内所有 field 的 varint 值
func collectVarints(buf []byte) []int64 {
	out := []int64{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch wire {
		case WireVarint:
			out = append(out, int64(r.ReadVarint()))
		case WireLen:
			out = append(out, collectVarints(r.ReadBytes())...)
		case WireI64:
			r.pos += 8
		case WireI32:
			r.pos += 4
		default:
			r.pos = len(r.buf)
		}
		return true
	})
	return out
}

// ExtractTopLevelField7String 从 Enter 响应的顶层原始字节中提取 field7 (string)。
// 主动加好友时服务端会在 Enter 响应顶层回显
// 32hex 会话 nonce（该字段不在我们的 schema 中，需直接扫 wire）。
func ExtractTopLevelField7String(raw []byte) string {
	r := NewReader(raw)
	for r.More() {
		field, wire := r.ReadTag()
		switch wire {
		case WireVarint:
			r.ReadVarint()
		case WireLen:
			data := r.ReadBytes()
			if field == 7 {
				return string(data)
			}
		case WireI64:
			r.pos += 8
		case WireI32:
			r.pos += 4
		default:
			return ""
		}
	}
	return ""
}
