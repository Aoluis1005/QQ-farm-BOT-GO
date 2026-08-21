package proto

// gamepb.interactpb 访客互动记录编解码。
// 多服务路由候选：
//	gamepb.interactpb.InteractService   InteractRecords / GetInteractRecords
//	gamepb.interactpb.VisitorService    InteractRecords / GetInteractRecords
var InteractRecordCandidates = [][2]string{
	{"gamepb.interactpb.InteractService", "InteractRecords"},
	{"gamepb.interactpb.InteractService", "GetInteractRecords"},
	{"gamepb.interactpb.VisitorService", "InteractRecords"},
	{"gamepb.interactpb.VisitorService", "GetInteractRecords"},
}

// InteractRecord 互动记录
type InteractRecord struct {
	ServerTime int64
	ActionType int32
	VisitorGID int64
	Nick       string
	AvatarURL  string
	CropID     int32
	CropCount  int32
	Times      int32
	FromType   int32
	Level      int32
	LandID     int32 // 来自 extra.land_id
	Flag1      int32
	Flag2      int32
}

func (rec *InteractRecord) decode(buf []byte) {
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			rec.ServerTime = r.ReadInt64()
		case 2:
			rec.ActionType = int32(r.ReadInt64())
		case 3:
			rec.VisitorGID = r.ReadInt64()
		case 4:
			rec.Nick = r.ReadString()
		case 5:
			rec.AvatarURL = r.ReadString()
		case 6:
			rec.CropID = int32(r.ReadInt64())
		case 7:
			rec.CropCount = int32(r.ReadInt64())
		case 8:
			rec.Times = int32(r.ReadInt64())
		case 9:
			rec.FromType = int32(r.ReadInt64())
		case 10:
			rec.Level = int32(r.ReadInt64())
		case 11:
			if wire == WireLen {
				er := NewReader(r.ReadBytes())
				er.EachField(func(ef, ew int, er *Reader) bool {
					switch ef {
					case 1:
						rec.LandID = int32(er.ReadInt64())
					case 2:
						rec.Flag1 = int32(er.ReadInt64())
					case 3:
						rec.Flag2 = int32(er.ReadInt64())
					default:
						er.Skip(ew)
					}
					return true
				})
			} else {
				r.Skip(wire)
			}
		default:
			r.Skip(wire)
		}
		return true
	})
}

// DecodeInteractRecordsReply 解析 InteractRecords 响应：records=1
func DecodeInteractRecordsReply(buf []byte) []*InteractRecord {
	out := []*InteractRecord{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			rec := &InteractRecord{}
			rec.decode(r.ReadBytes())
			out = append(out, rec)
		} else {
			r.Skip(wire)
		}
		return true
	})
	return out
}

// EncodeInteractRecordsRequest 空请求
func EncodeInteractRecordsRequest() []byte { return nil }
