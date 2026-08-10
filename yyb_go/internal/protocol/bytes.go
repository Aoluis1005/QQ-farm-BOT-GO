package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

func varint(n uint64) []byte {
	var out []byte
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			return out
		}
	}
}

func readVarint(data []byte, i int) (uint64, int, error) {
	var n uint64
	var shift uint
	for i < len(data) {
		b := data[i]
		i++
		n |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return n, i, nil
		}
		shift += 7
		if shift > 63 {
			return 0, i, fmt.Errorf("varint overflow")
		}
	}
	return 0, i, fmt.Errorf("truncated varint")
}

func pbLen(field int, data []byte) []byte {
	out := []byte{byte((field << 3) | 2)}
	out = append(out, varint(uint64(len(data)))...)
	out = append(out, data...)
	return out
}

func pbVar(field int, n uint64) []byte {
	out := []byte{byte((field << 3) | 0)}
	out = append(out, varint(n)...)
	return out
}

func pbParse(data []byte) map[int]any {
	out := map[int]any{}
	i := 0
	for i < len(data) {
		tag, ni, err := readVarint(data, i)
		if err != nil {
			break
		}
		i = ni
		fn, wt := int(tag>>3), int(tag&7)
		switch wt {
		case 0:
			v, ni, err := readVarint(data, i)
			if err != nil {
				return out
			}
			i = ni
			out[fn] = int64(v)
		case 2:
			ln, ni, err := readVarint(data, i)
			if err != nil {
				return out
			}
			i = ni
			if i+int(ln) > len(data) {
				return out
			}
			raw := append([]byte(nil), data[i:i+int(ln)]...)
			i += int(ln)
			out[fn] = raw
		default:
			return out
		}
	}
	return out
}

func protobufToMap(data []byte) map[string]any {
	out := map[string]any{}
	i := 0
	for i < len(data) {
		tag, ni, err := readVarint(data, i)
		if err != nil {
			break
		}
		i = ni
		fn, wt := int(tag>>3), int(tag&7)
		key := fmt.Sprintf("%d", fn)
		switch wt {
		case 0:
			v, ni, err := readVarint(data, i)
			if err != nil {
				return out
			}
			i = ni
			out[key] = v
		case 2:
			ln, ni, err := readVarint(data, i)
			if err != nil {
				return out
			}
			i = ni
			if i+int(ln) > len(data) {
				return out
			}
			raw := data[i : i+int(ln)]
			i += int(ln)
			if utf8.Valid(raw) {
				s := string(raw)
				if isPrintableShort(s) {
					out[key] = s
				} else {
					out[key] = map[string]any{"__bytes__": fmt.Sprintf("%x", raw)}
				}
			} else {
				sub := protobufToMap(raw)
				if len(sub) > 0 {
					out[key] = sub
				} else {
					out[key] = map[string]any{"__bytes__": fmt.Sprintf("%x", raw)}
				}
			}
		default:
			return out
		}
	}
	return out
}

func isPrintableShort(s string) bool {
	if len(s) > 0 && bytes.HasPrefix([]byte(s), []byte("{")) {
		return true
	}
	if len(s) >= 200 {
		return false
	}
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 32 || r >= 127 {
			return false
		}
	}
	return true
}

func lz4AllLiteral(data []byte) []byte {
	n := len(data)
	if n < 15 {
		return append([]byte{byte(n << 4)}, data...)
	}
	out := []byte{0xf0}
	rem := n - 15
	for rem >= 255 {
		out = append(out, 255)
		rem -= 255
	}
	out = append(out, byte(rem))
	out = append(out, data...)
	return out
}

func lz4Decompress(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data)*2)
	i := 0
	for i < len(data) {
		tok := data[i]
		i++
		litLen := int(tok >> 4)
		if litLen == 15 {
			for i < len(data) && data[i] == 255 {
				litLen += 255
				i++
			}
			if i >= len(data) {
				return nil, fmt.Errorf("truncated lz4 literal length")
			}
			litLen += int(data[i])
			i++
		}
		if i+litLen > len(data) {
			return nil, fmt.Errorf("truncated lz4 literals")
		}
		out = append(out, data[i:i+litLen]...)
		i += litLen
		if i >= len(data) {
			break
		}
		if i+2 > len(data) {
			return nil, fmt.Errorf("truncated lz4 offset")
		}
		off := int(binary.LittleEndian.Uint16(data[i : i+2]))
		i += 2
		if off <= 0 || off > len(out) {
			return nil, fmt.Errorf("invalid lz4 offset")
		}
		matchLen := int(tok&0x0f) + 4
		if tok&0x0f == 15 {
			for i < len(data) && data[i] == 255 {
				matchLen += 255
				i++
			}
			if i >= len(data) {
				return nil, fmt.Errorf("truncated lz4 match length")
			}
			matchLen += int(data[i])
			i++
		}
		start := len(out) - off
		for j := 0; j < matchLen; j++ {
			out = append(out, out[start+j])
		}
	}
	return out, nil
}

func safeBytes(v any) []byte {
	switch x := v.(type) {
	case []byte:
		return x
	case string:
		return []byte(x)
	default:
		return nil
	}
}
