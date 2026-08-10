package proto

// gamepb.friendpb.FriendService 相关编解码。
// 对齐 Node core/src/proto/friendpb.proto 与 utils/proto.js。

// GetApplicationsRequest {}（空请求）
func EncodeGetApplicationsRequest() []byte { return nil }

// EncodeAcceptFriendsRequest 同意好友申请 { friend_gids=1 repeated }
func EncodeAcceptFriendsRequest(gids []int64) []byte {
	b := NewBuilder()
	for _, g := range gids {
		b.FieldInt64(1, g)
	}
	return b.Bytes()
}

// EncodeDelFriendRequest 删除好友 { friend_gid=1 }
func EncodeDelFriendRequest(gid int64) []byte {
	b := NewBuilder()
	b.FieldInt64(1, gid)
	return b.Bytes()
}

// EncodeGetGameFriendsRequest 按 GID 批量获取好友 { gids=1 repeated }（QQ 平台）
func EncodeGetGameFriendsRequest(gids []int64) []byte {
	b := NewBuilder()
	for _, g := range gids {
		b.FieldInt64(1, g)
	}
	return b.Bytes()
}

// EncodeGetAllRequest {}（GetAll 空请求）
func EncodeGetAllRequest() []byte { return nil }

// FriendPlant 好友农场摘要（friendpb.Plant）
type FriendPlant struct {
	DryTimeSec    int64
	WeedTimeSec   int64
	InsectTimeSec int64
	RipeTimeSec   int64
	RipeFruitID   int64
	StealPlantNum int64 // 可偷数量
	DryNum        int64
	WeedNum       int64
	InsectNum     int64
}

func (p *FriendPlant) decode(buf []byte) {
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			p.DryTimeSec = r.ReadInt64()
		case 2:
			p.WeedTimeSec = r.ReadInt64()
		case 3:
			p.InsectTimeSec = r.ReadInt64()
		case 4:
			p.RipeTimeSec = r.ReadInt64()
		case 5:
			p.RipeFruitID = r.ReadInt64()
		case 6:
			p.StealPlantNum = r.ReadInt64()
		case 7:
			p.DryNum = r.ReadInt64()
		case 8:
			p.WeedNum = r.ReadInt64()
		case 9:
			p.InsectNum = r.ReadInt64()
		default:
			r.Skip(wire)
		}
		return true
	})
}

// GameFriend 好友信息（friendpb.GameFriend）
type GameFriend struct {
	GID      int64
	OpenID   string
	Name     string
	AvatarURL string
	Remark   string
	Level    int64
	Gold     int64
	Plant    *FriendPlant
}

func (f *GameFriend) decode(buf []byte) {
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			f.GID = r.ReadInt64()
		case 2:
			f.OpenID = r.ReadString()
		case 3:
			f.Name = r.ReadString()
		case 4:
			f.AvatarURL = r.ReadString()
		case 5:
			f.Remark = r.ReadString()
		case 6:
			f.Level = r.ReadInt64()
		case 7:
			f.Gold = r.ReadInt64()
		case 9:
			if wire == WireLen {
				p := &FriendPlant{}
				p.decode(r.ReadBytes())
				f.Plant = p
			} else {
				r.Skip(wire)
			}
		default:
			r.Skip(wire)
		}
		return true
	})
}

// GetAllReply 获取所有好友响应
// GetAllReply: game_friends=1(repeated), application_count=3
type GetAllReply struct {
	Friends          []*GameFriend
	ApplicationCount int64
}

func DecodeGetAllReply(buf []byte) *GetAllReply {
	rep := &GetAllReply{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			if wire == WireLen {
				f := &GameFriend{}
				f.decode(r.ReadBytes())
				rep.Friends = append(rep.Friends, f)
			} else {
				r.Skip(wire)
			}
		case 3:
			rep.ApplicationCount = r.ReadInt64()
		default:
			r.Skip(wire)
		}
		return true
	})
	return rep
}

// DecodeGetGameFriendsReply 复用 GetAllReply 结构（均含 game_friends=1）
func DecodeGetGameFriendsReply(buf []byte) *GetAllReply {
	return DecodeGetAllReply(buf)
}

// FriendApplication 好友申请（friendpb.Application）
// Application: gid=1, time_at=2, open_id=3, name=4, avatar_url=5, level=6
type FriendApplication struct {
	GID       int64
	TimeAt    int64
	OpenID    string
	Name      string
	AvatarURL string
	Level     int64
}

func (a *FriendApplication) decode(buf []byte) {
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			a.GID = r.ReadInt64()
		case 2:
			a.TimeAt = r.ReadInt64()
		case 3:
			a.OpenID = r.ReadString()
		case 4:
			a.Name = r.ReadString()
		case 5:
			a.AvatarURL = r.ReadString()
		case 6:
			a.Level = r.ReadInt64()
		default:
			r.Skip(wire)
		}
		return true
	})
}

// GetApplicationsReply 好友申请列表响应
// GetApplicationsReply: applications=1(repeated), block_applications=2
type GetApplicationsReply struct {
	Applications     []*FriendApplication
	BlockApplications bool
}

func DecodeGetApplicationsReply(buf []byte) *GetApplicationsReply {
	rep := &GetApplicationsReply{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			if wire == WireLen {
				a := &FriendApplication{}
				a.decode(r.ReadBytes())
				rep.Applications = append(rep.Applications, a)
			} else {
				r.Skip(wire)
			}
		case 2:
			rep.BlockApplications = r.ReadInt64() != 0
		default:
			r.Skip(wire)
		}
		return true
	})
	return rep
}

// DecodeAcceptFriendsReply 同意后的好友列表（AcceptFriendsReply: friends=1）
func DecodeAcceptFriendsReply(buf []byte) []*GameFriend {
	out := []*GameFriend{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			f := &GameFriend{}
			f.decode(r.ReadBytes())
			out = append(out, f)
		} else {
			r.Skip(wire)
		}
		return true
	})
	return out
}
