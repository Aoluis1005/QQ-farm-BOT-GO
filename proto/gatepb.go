package proto

// gatepb.Message / gatepb.Meta 编解码
// 对应 proto/game.proto 的 gatepb 定义

// MessageType 枚举
const (
	MsgTypeNone     = 0
	MsgTypeRequest  = 1
	MsgTypeResponse = 2
	MsgTypeNotify   = 3
)

// Meta 消息元信息
type Meta struct {
	ServiceName  string
	MethodName   string
	MessageType  int32
	ClientSeq    int64
	ServerSeq    int64
	ErrorCode    int64
	ErrorMessage string
	Metadata     map[string][]byte
}

// Message 每个 WS 帧的外壳
type Message struct {
	Meta      *Meta
	Body      []byte
	AuthToken string
}

// Encode 编码 Meta 为 bytes
func (m *Meta) Encode() []byte {
	b := NewBuilder()
	b.FieldString(1, m.ServiceName)
	b.FieldString(2, m.MethodName)
	b.FieldInt32(3, m.MessageType)
	b.FieldInt64(4, m.ClientSeq)
	b.FieldInt64(5, m.ServerSeq)
	b.FieldInt64(6, m.ErrorCode)
	b.FieldString(7, m.ErrorMessage)
	if len(m.Metadata) > 0 {
		for k, v := range m.Metadata {
			entry := NewBuilder()
			entry.FieldString(1, k)
			entry.FieldBytes(2, v)
			b.FieldMessage(8, entry.Bytes())
		}
	}
	return b.Bytes()
}

// Decode 解码 Meta
func (m *Meta) Decode(buf []byte) {
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			m.ServiceName = r.ReadString()
		case 2:
			m.MethodName = r.ReadString()
		case 3:
			m.MessageType = int32(r.ReadInt64())
		case 4:
			m.ClientSeq = r.ReadInt64()
		case 5:
			m.ServerSeq = r.ReadInt64()
		case 6:
			m.ErrorCode = r.ReadInt64()
		case 7:
			m.ErrorMessage = r.ReadString()
		case 8:
			// map entry
			if wire == WireLen {
				sub := r.ReadBytes()
				if m.Metadata == nil {
					m.Metadata = map[string][]byte{}
				}
				var k string
				var v []byte
				er := NewReader(sub)
				er.EachField(func(f, w int, r *Reader) bool {
					switch f {
					case 1:
						k = r.ReadString()
					case 2:
						v = r.ReadBytes()
					}
					return true
				})
				m.Metadata[k] = v
			} else {
				r.Skip(wire)
			}
		default:
			r.Skip(wire)
		}
		return true
	})
}

// EncodeMessage 编码 GateMessage
func EncodeMessage(meta *Meta, body []byte, authToken string) []byte {
	b := NewBuilder()
	if meta != nil {
		b.FieldMessage(1, meta.Encode())
	}
	b.FieldBytes(2, body)
	b.FieldString(3, authToken)
	return b.Bytes()
}

// DecodeMessage 解码 GateMessage
func DecodeMessage(buf []byte) *Message {
	msg := &Message{}
	r := NewReader(buf)
	r.EachField(func(field, wire int, r *Reader) bool {
		switch field {
		case 1:
			if wire == WireLen {
				sub := r.ReadBytes()
				m := &Meta{}
				m.Decode(sub)
				msg.Meta = m
			} else {
				r.Skip(wire)
			}
		case 2:
			msg.Body = r.ReadBytes()
		case 3:
			msg.AuthToken = r.ReadString()
		default:
			r.Skip(wire)
		}
		return true
	})
	return msg
}
