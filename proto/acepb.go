package proto

// gamepb.acepb ACE 反作弊上报编解码（对齐 Node core/src/proto/acepb.proto）
//
//	message AntiDataRequest { bytes data = 1; }
//	message AntiDataReply   { bytes data = 1; }

// EncodeAntiDataRequest 构造 AntiDataRequest{data=1}
func EncodeAntiDataRequest(data []byte) []byte {
	b := NewBuilder()
	if len(data) > 0 {
		b.FieldBytes(1, data)
	}
	return b.Bytes()
}

// DecodeAntiDataReply 解析 AntiDataReply{data=1}，返回服务端回灌数据
func DecodeAntiDataReply(buf []byte) []byte {
	r := NewReader(buf)
	var out []byte
	r.EachField(func(field, wire int, r *Reader) bool {
		if field == 1 && wire == WireLen {
			out = r.ReadBytes()
			return false
		}
		r.Skip(wire)
		return true
	})
	return out
}
