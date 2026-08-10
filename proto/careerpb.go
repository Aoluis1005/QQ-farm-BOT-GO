package proto

// gamepb.careerpb 生涯统计编解码
// 对齐 Node core/src/services/career-api.js 的原始 protobuf 解码 (decodeCareerReplyRaw)
// CareerInfoGetReply 真实结构：
//   field 1  = repeated CareerStatItem  (f1=fruit_id, f2=count)
//   field 2  = varint stats_total   总收获数
//   field 3  = varint stats_count   备用统计
//   field 4  = string 玩家昵称
//   field 5  = string 玩家头像 URL
//   field 9  = varint Lv
//   field 10 = varint 经验
//   field 11 = varint gid 角色编号
//   field 12 = repeated CareerLevelStat (f1=fruit_id, f2=count, f4=level)
//   field 13 = varint achieved_levels
//   field 15 = string openid

// EncodeCareerInfoGetRequest 生涯统计请求（空 body，对齐 Node CareerInfoGetRequest.create({})）
func EncodeCareerInfoGetRequest() []byte {
	return []byte{}
}

// CareerStatItem 收获排行条目
type CareerStatItem struct {
	FruitID int64
	Count   int64
}

// CareerLevelStat 作物等级条目
type CareerLevelStat struct {
	FruitID int64
	Count   int64
	Level   int64
}

// CareerInfoGetReply 生涯统计响应
type CareerInfoGetReply struct {
	Items          []*CareerStatItem
	LevelStats     []*CareerLevelStat
	StatsTotal     int64
	StatsCount     int64
	Name           string
	Avatar         string
	Level          int64
	Exp            int64
	GID            int64
	AchievedLevels int64
	OpenID         string
}

// DecodeCareerInfoGetReply 原始 protobuf 解码
func DecodeCareerInfoGetReply(buf []byte) *CareerInfoGetReply {
	reply := &CareerInfoGetReply{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if wire == WireVarint {
			switch field {
			case 2:
				reply.StatsTotal = r.ReadInt64()
			case 3:
				reply.StatsCount = r.ReadInt64()
			case 9:
				reply.Level = r.ReadInt64()
			case 10:
				reply.Exp = r.ReadInt64()
			case 11:
				reply.GID = r.ReadInt64()
			case 13:
				reply.AchievedLevels = r.ReadInt64()
			default:
				r.Skip(wire)
			}
			return true
		}
		if wire != WireLen {
			r.Skip(wire)
			return true
		}
		sub := r.ReadBytes()
		switch field {
		case 4:
			reply.Name = printableString(sub)
		case 5:
			reply.Avatar = printableString(sub)
		case 15:
			reply.OpenID = printableString(sub)
		case 1: // 嵌套 CareerStatItem
			if it := decodeCareerStatItem(sub); it != nil {
				reply.Items = append(reply.Items, it)
			}
		case 12: // 嵌套 CareerLevelStat
			if it := decodeCareerLevelStat(sub); it != nil {
				reply.LevelStats = append(reply.LevelStats, it)
			}
		}
		return true
	})
	return reply
}

func decodeCareerStatItem(buf []byte) *CareerStatItem {
	it := &CareerStatItem{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if wire != WireVarint {
			r.Skip(wire)
			return true
		}
		switch field {
		case 1:
			it.FruitID = r.ReadInt64()
		case 2:
			it.Count = r.ReadInt64()
		default:
			r.Skip(wire)
		}
		return true
	})
	if it.FruitID <= 0 { // 对齐 Node：无 fruit_id 不入列
		return nil
	}
	return it
}

func decodeCareerLevelStat(buf []byte) *CareerLevelStat {
	it := &CareerLevelStat{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if wire != WireVarint {
			r.Skip(wire)
			return true
		}
		switch field {
		case 1:
			it.FruitID = r.ReadInt64()
		case 2:
			it.Count = r.ReadInt64()
		case 4:
			it.Level = r.ReadInt64()
		default:
			r.Skip(wire)
		}
		return true
	})
	if it.FruitID <= 0 {
		return nil
	}
	return it
}

// printableString 对齐 Node isPrintable：仅接受可打印 ASCII（1~255 字节）
func printableString(b []byte) string {
	if len(b) == 0 || len(b) >= 256 {
		return ""
	}
	for _, c := range b {
		if c < 32 || c > 126 {
			return ""
		}
	}
	return string(b)
}
