package tle

import (
	"errors"
	"fmt"
)

// EncodeExecFragment encodes the plaintext an encrypted-batch submission
// carries: a MsgShieldedExec with only `inner_message` (field 2, an Any),
// `proof` (field 3, the operation proof) and `nullifier` (field 4, the
// operation nullifier) set. The chain restores every other field from the
// PendingShieldedOp at execution, so nothing else belongs in here. The
// operation nullifier is sealed with the content so nothing public at
// submission links the submission to the executed operation.
//
// Hand-rolled protobuf so web clients (tools/zk/wasm) do not have to link
// the SDK's generated types; the output is byte-identical to marshaling
// types.MsgShieldedExec{InnerMessage: &Any{TypeUrl, Value}, Proof, Nullifier}.
func EncodeExecFragment(innerTypeURL string, innerValue, proof, nullifier []byte) ([]byte, error) {
	if innerTypeURL == "" {
		return nil, errors.New("tle: inner type url is empty")
	}
	anyBytes := appendBytesField(nil, 1, []byte(innerTypeURL))
	anyBytes = appendBytesField(anyBytes, 2, innerValue)

	out := appendBytesField(nil, 2, anyBytes)
	out = appendBytesField(out, 3, proof)
	out = appendBytesField(out, 4, nullifier)
	return out, nil
}

// DecodeExecFragment is the inverse of EncodeExecFragment, for tools and
// tests that want to inspect a plaintext without the SDK codec.
func DecodeExecFragment(bz []byte) (innerTypeURL string, innerValue, proof, nullifier []byte, err error) {
	fields, err := parseFields(bz)
	if err != nil {
		return "", nil, nil, nil, err
	}
	anyBytes, ok := fields[2]
	if !ok {
		return "", nil, nil, nil, errors.New("tle: fragment has no inner_message")
	}
	anyFields, err := parseFields(anyBytes)
	if err != nil {
		return "", nil, nil, nil, fmt.Errorf("tle: inner_message: %w", err)
	}
	return string(anyFields[1]), anyFields[2], fields[3], fields[4], nil
}

func appendBytesField(dst []byte, field int, v []byte) []byte {
	// Proto3 omits empty length-delimited fields.
	if len(v) == 0 {
		return dst
	}
	dst = appendVarint(dst, uint64(field)<<3|2)
	dst = appendVarint(dst, uint64(len(v)))
	return append(dst, v...)
}

func appendVarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

// parseFields returns the last occurrence of each length-delimited field.
// Varint and fixed fields are skipped; groups are rejected.
func parseFields(bz []byte) (map[int][]byte, error) {
	fields := map[int][]byte{}
	for len(bz) > 0 {
		tag, n := readVarint(bz)
		if n <= 0 {
			return nil, errors.New("tle: bad tag")
		}
		bz = bz[n:]
		field, wire := int(tag>>3), tag&7
		switch wire {
		case 0:
			_, n = readVarint(bz)
			if n <= 0 {
				return nil, errors.New("tle: bad varint")
			}
			bz = bz[n:]
		case 1:
			if len(bz) < 8 {
				return nil, errors.New("tle: short fixed64")
			}
			bz = bz[8:]
		case 2:
			l, n := readVarint(bz)
			if n <= 0 || uint64(len(bz)-n) < l {
				return nil, errors.New("tle: bad length")
			}
			fields[field] = bz[n : n+int(l)]
			bz = bz[n+int(l):]
		case 5:
			if len(bz) < 4 {
				return nil, errors.New("tle: short fixed32")
			}
			bz = bz[4:]
		default:
			return nil, fmt.Errorf("tle: unsupported wire type %d", wire)
		}
	}
	return fields, nil
}

func readVarint(bz []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(bz) && i < 10; i++ {
		v |= uint64(bz[i]&0x7f) << (7 * uint(i))
		if bz[i] < 0x80 {
			return v, i + 1
		}
	}
	return 0, -1
}
