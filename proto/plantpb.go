package proto

// gamepb.plantpb 农场数据编解码（对齐 proto/plantpb.proto）

// EncodeAllLandsRequest 获取所有地块请求
func EncodeAllLandsRequest(hostGid int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, hostGid) // host_gid, 0=自己
	return b.Bytes()
}

// PlantPhase 阶段枚举
const (
	PhaseUnknown  = 0
	PhaseSeed     = 1
	PhaseGerm     = 2
	PhaseSmallLf  = 3
	PhaseLargeLf  = 4
	PhaseBlooming = 5
	PhaseMature   = 6
	PhaseDead     = 7
)

// PlantPhaseInfo 生长阶段
type PlantPhaseInfo struct {
	Phase     int32
	BeginTime int64
	DryTime   int64
	WeedsTime int64
	InsectTime int64
}

func (p *PlantPhaseInfo) decode(buf []byte) {
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			p.Phase = int32(r.ReadInt64())
		case 2:
			p.BeginTime = r.ReadInt64()
		case 6:
			p.DryTime = r.ReadInt64()
		case 7:
			p.WeedsTime = r.ReadInt64()
		case 8:
			p.InsectTime = r.ReadInt64()
		default:
			r.Skip(wire)
		}
		return true
	})
}

// PlantInfo 作物信息
type PlantInfo struct {
	ID           int64
	Name         string
	Phases       []*PlantPhaseInfo
	DryNum       int64 // 缺水次数
	StoleNum     int64
	FruitID      int64
	FruitNum     int64
	WeedOwners   []int64
	InsectOwners []int64
	Stealers     []int64
	GrowSec      int64
	Stealable    bool
	LeftFruitNum int64
	IsNudged     bool
	WeedNum      int64 // 有草地块数（当前 proto 未下发，默认0；friend_service 依赖字段存在）
	InsectNum    int64 // 有虫地块数
}

func (p *PlantInfo) decode(buf []byte) {
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			p.ID = r.ReadInt64()
		case 2:
			p.Name = r.ReadString()
		case 4:
			if wire == WireLen {
				sub := r.ReadBytes()
				ph := &PlantPhaseInfo{}
				ph.decode(sub)
				p.Phases = append(p.Phases, ph)
			} else {
				r.Skip(wire)
			}
		case 6:
			p.DryNum = r.ReadInt64()
		case 9:
			p.StoleNum = r.ReadInt64()
		case 10:
			p.FruitID = r.ReadInt64()
		case 11:
			p.FruitNum = r.ReadInt64()
		case 12:
			p.WeedOwners = append(p.WeedOwners, r.ReadInt64())
		case 13:
			p.InsectOwners = append(p.InsectOwners, r.ReadInt64())
		case 14:
			p.Stealers = append(p.Stealers, r.ReadInt64())
		case 15:
			p.GrowSec = r.ReadInt64()
		case 16:
			p.Stealable = r.ReadInt64() != 0
		case 18:
			p.LeftFruitNum = r.ReadInt64()
		case 21:
			p.IsNudged = r.ReadInt64() != 0
		default:
			r.Skip(wire)
		}
		return true
	})
}

// LandInfo 地块信息
type LandInfo struct {
	ID       int64
	Unlocked bool
	Level    int64
	MaxLevel int64
	Plant    *PlantInfo
}

func (l *LandInfo) decode(buf []byte) {
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			l.ID = r.ReadInt64()
		case 2:
			l.Unlocked = r.ReadInt64() != 0
		case 3:
			l.Level = r.ReadInt64()
		case 4:
			l.MaxLevel = r.ReadInt64()
		case 10:
			if wire == WireLen {
				sub := r.ReadBytes()
				p := &PlantInfo{}
				p.decode(sub)
				l.Plant = p
			} else {
				r.Skip(wire)
			}
		default:
			r.Skip(wire)
		}
		return true
	})
}

// AllLandsReply 所有地块响应
type AllLandsReply struct {
	Lands []*LandInfo
}

// DecodeAllLandsReply 解析所有地块
func DecodeAllLandsReply(buf []byte) *AllLandsReply {
	rep := &AllLandsReply{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			sub := r.ReadBytes()
			l := &LandInfo{}
			l.decode(sub)
			rep.Lands = append(rep.Lands, l)
		} else {
			r.Skip(wire)
		}
		return true
	})
	return rep
}

// ============ 农场操作请求编码（gamepb.plantpb.PlantService） ============

// EncodeHarvestRequest 收获
func EncodeHarvestRequest(landIDs []int64, hostGid int64, isAll bool) []byte {
	b := NewBuilder()
	for _, id := range landIDs {
		b.FieldInt64(1, id)
	}
	if hostGid != 0 {
		b.FieldInt64(2, hostGid)
	}
	if isAll {
		b.FieldBool(3, true)
	}
	return b.Bytes()
}

// EncodeFarmingRequest 一键务农
func EncodeFarmingRequest(landIDs []int64, hostGid int64) []byte {
	b := NewBuilder()
	for _, id := range landIDs {
		b.FieldInt64(1, id)
	}
	if hostGid != 0 {
		b.FieldInt64(2, hostGid)
	}
	return b.Bytes()
}

// EncodeFertilizeRequest 施肥/催熟
func EncodeFertilizeRequest(landIDs []int64, fertilizerID int64) []byte {
	b := NewBuilder()
	for _, id := range landIDs {
		b.FieldInt64(1, id)
	}
	if fertilizerID != 0 {
		b.FieldInt64(2, fertilizerID)
	}
	return b.Bytes()
}

// EncodeRemovePlantRequest 铲除
func EncodeRemovePlantRequest(landIDs []int64) []byte {
	b := NewBuilder()
	for _, id := range landIDs {
		b.FieldInt64(1, id)
	}
	return b.Bytes()
}

// EncodeUpgradeLandRequest 升级土地
func EncodeUpgradeLandRequest(landID int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, landID)
	return b.Bytes()
}

// EncodeUnlockLandRequest 解锁土地
func EncodeUnlockLandRequest(landID int64, doShared bool) []byte {
	b := NewBuilder()
	b.FieldInt64(1, landID)
	if doShared {
		b.FieldBool(2, true)
	}
	return b.Bytes()
}

// ============ 好友农场操作请求编码（对齐 Node friend-operation-limits.js） ============
// 均由 gamepb.plantpb.PlantService 投递，字段：land_ids / host_gid。

// 浇水（WaterLandRequest: land_ids=1, host_gid=2）
func EncodeWaterLandRequest(landIDs []int64, hostGid int64) []byte {
	b := NewBuilder()
	for _, id := range landIDs {
		b.FieldInt64(1, id)
	}
	if hostGid != 0 {
		b.FieldInt64(2, hostGid)
	}
	return b.Bytes()
}

// 除草（WeedOutRequest: land_ids=1, host_gid=2）
func EncodeWeedOutRequest(landIDs []int64, hostGid int64) []byte {
	b := NewBuilder()
	for _, id := range landIDs {
		b.FieldInt64(1, id)
	}
	if hostGid != 0 {
		b.FieldInt64(2, hostGid)
	}
	return b.Bytes()
}

// 除虫（InsecticideRequest: land_ids=1, host_gid=2）
func EncodeInsecticideRequest(landIDs []int64, hostGid int64) []byte {
	b := NewBuilder()
	for _, id := range landIDs {
		b.FieldInt64(1, id)
	}
	if hostGid != 0 {
		b.FieldInt64(2, hostGid)
	}
	return b.Bytes()
}

// 放虫（PutInsectsRequest: host_gid=1, land_ids=2）
func EncodePutInsectsRequest(hostGid int64, landIDs []int64) []byte {
	b := NewBuilder()
	if hostGid != 0 {
		b.FieldInt64(1, hostGid)
	}
	for _, id := range landIDs {
		b.FieldInt64(2, id)
	}
	return b.Bytes()
}

// 放草（PutWeedsRequest: host_gid=1, land_ids=2）
func EncodePutWeedsRequest(hostGid int64, landIDs []int64) []byte {
	b := NewBuilder()
	if hostGid != 0 {
		b.FieldInt64(1, hostGid)
	}
	for _, id := range landIDs {
		b.FieldInt64(2, id)
	}
	return b.Bytes()
}

// CheckCanOperateRequest: host_gid=1, operation_id=2
func EncodeCheckCanOperateRequest(hostGid, operationID int64) []byte {
	b := NewBuilder()
	if hostGid != 0 {
		b.FieldInt64(1, hostGid)
	}
	if operationID != 0 {
		b.FieldInt64(2, operationID)
	}
	return b.Bytes()
}

// DecodeOpsLandList 解析操作类响应中的地块列表（field 1 = repeated LandInfo）。
// 对齐 Harvest/WaterLand/WeedOut/Insecticide/PutInsects/PutWeeds 的 Reply.land。
func DecodeOpsLandList(buf []byte) []*LandInfo {
	out := []*LandInfo{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			l := &LandInfo{}
			l.decode(r.ReadBytes())
			out = append(out, l)
		} else {
			r.Skip(wire)
		}
		return true
	})
	return out
}

