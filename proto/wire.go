package proto

// protowire 工具：手写 protobuf 编解码（标准库实现）

// wireType 常量
const (
	WireVarint = 0
	WireI64    = 1
	WireLen    = 2
	WireI32    = 5
)

// Builder 编码器
type Builder struct {
	buf []byte
}

func NewBuilder() *Builder { return &Builder{buf: make([]byte, 0, 128)} }

func (b *Builder) Bytes() []byte { return b.buf }

func (b *Builder) appendVarint(x uint64) {
	for x >= 0x80 {
		b.buf = append(b.buf, byte(x)|0x80)
		x >>= 7
	}
	b.buf = append(b.buf, byte(x))
}

func (b *Builder) tag(field int, wire int) { b.appendVarint(uint64(field)<<3 | uint64(wire)) }

// FieldString 写入 string 字段
func (b *Builder) FieldString(field int, s string) {
	if s == "" {
		return
	}
	b.tag(field, WireLen)
	b.appendVarint(uint64(len(s)))
	b.buf = append(b.buf, s...)
}

// FieldBytes 写入 bytes 字段
func (b *Builder) FieldBytes(field int, data []byte) {
	if len(data) == 0 {
		return
	}
	b.tag(field, WireLen)
	b.appendVarint(uint64(len(data)))
	b.buf = append(b.buf, data...)
}

// FieldInt64 写入 int64/uint64 字段
func (b *Builder) FieldInt64(field int, v int64) {
	if v == 0 {
		return
	}
	b.tag(field, WireVarint)
	b.appendVarint(uint64(v))
}

// FieldInt64Always 写入 int64 字段（非跳过0）
func (b *Builder) FieldInt64Always(field int, v int64) {
	b.tag(field, WireVarint)
	b.appendVarint(uint64(v))
}

// FieldUint64 写入 uint64 字段（非跳过0）
func (b *Builder) FieldUint64Always(field int, v uint64) {
	b.tag(field, WireVarint)
	b.appendVarint(v)
}

// FieldInt32 写入 int32 字段
func (b *Builder) FieldInt32(field int, v int32) {
	if v == 0 {
		return
	}
	b.tag(field, WireVarint)
	b.appendVarint(uint64(uint32(v)))
}

// FieldBool 写入 bool 字段
func (b *Builder) FieldBool(field int, v bool) {
	if !v {
		return
	}
	b.tag(field, WireVarint)
	b.buf = append(b.buf, 1)
}

// FieldMessage 写入嵌套 message 字段
func (b *Builder) FieldMessage(field int, sub []byte) {
	if len(sub) == 0 {
		return
	}
	b.tag(field, WireLen)
	b.appendVarint(uint64(len(sub)))
	b.buf = append(b.buf, sub...)
}

// Reader 解码器
type Reader struct {
	buf []byte
	pos int
}

func NewReader(buf []byte) *Reader { return &Reader{buf: buf} }

// More 是否还有未读
func (r *Reader) More() bool { return r.pos < len(r.buf) }

func (r *Reader) error() error { return nil } // 简化：越界返回空

// ReadVarint 读 varint（越界安全：pos 到缓冲区末尾即停，不再 panic）
// 【2026-08-20 修复】原实现 r.buf[r.pos] 无边界检查：服务端推送损坏/未知字段导致
// pos 越界后，下一次 ReadVarint 直接 panic 拖垮整个进程（DecodeItemNotify 实崩案例）。
func (r *Reader) ReadVarint() uint64 {
	var x uint64
	var shift uint
	for r.pos < len(r.buf) {
		b := r.buf[r.pos]
		r.pos++
		if shift < 64 {
			x |= uint64(b&0x7f) << shift
		}
		if b&0x80 == 0 {
			break
		}
		shift += 7
	}
	return x
}

// ReadTag 读 tag，返回 field 和 wireType
func (r *Reader) ReadTag() (int, int) {
	v := r.ReadVarint()
	return int(v >> 3), int(v & 0x7)
}

// ReadBytes 读 length-delimited 原始字节（不含长度前缀已在调用前跳过）
func (r *Reader) ReadBytes() []byte {
	n := r.ReadVarint()
	if r.pos+int(n) > len(r.buf) {
		return nil
	}
	b := r.buf[r.pos : r.pos+int(n)]
	r.pos += int(n)
	return b
}

// ReadString 读 string
func (r *Reader) ReadString() string { return string(r.ReadBytes()) }

// ReadInt64 读 int64
func (r *Reader) ReadInt64() int64 { return int64(r.ReadVarint()) }

// AppendRepeatedInt64 读取 repeated int64 字段并追加到 dst。
// proto3 的 repeated 标量默认是 packed（wire=2，一个长度前缀里塞多个 varint），
// 但服务端也可能按非 packed（wire=0，每个元素一个 tag）下发。两种都要吃下，
// 否则 packed 情况下会把「长度前缀」当成数值读出来（旧实现就有这个隐患）。
func (r *Reader) AppendRepeatedInt64(wire int, dst []int64) []int64 {
	if wire == WireLen {
		sub := NewReader(r.ReadBytes())
		for sub.More() {
			dst = append(dst, sub.ReadInt64())
		}
		return dst
	}
	if wire == WireVarint {
		return append(dst, r.ReadInt64())
	}
	r.Skip(wire)
	return dst
}

// Skip 跳过字段（按 wireType）；任何 wire 类型都钳位 pos，防止越界
// 【2026-08-20 修复】WireLen 分支原来 r.pos += n 无上限，n 为损坏长度时 pos 越界 → 后续读 panic。
func (r *Reader) Skip(wire int) {
	switch wire {
	case WireVarint:
		r.ReadVarint()
	case WireI64:
		r.pos += 8
	case WireLen:
		n := r.ReadVarint()
		r.pos += int(n)
	case WireI32:
		r.pos += 4
	default:
		r.pos = len(r.buf)
	}
	if r.pos > len(r.buf) {
		r.pos = len(r.buf)
	}
}

// EachField 遍历所有字段：fn(field, wire, reader) 返回 false 停止
func (r *Reader) EachField(fn func(field, wire int, r *Reader) bool) {
	for r.More() {
		field, wire := r.ReadTag()
		if !fn(field, wire, r) {
			return
		}
	}
}
