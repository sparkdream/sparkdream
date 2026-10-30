package types

import (
	"fmt"
	"math"
	"unsafe"
)

// Integer is every built-in integer type.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// bounds returns T's range as (min as int64 when signed, max as uint64).
func bounds[T Integer]() (signed bool, minVal int64, maxVal uint64) {
	var z T
	bits := uint(unsafe.Sizeof(z)) * 8
	z--
	if z < 0 { // signed
		return true, -1 << (bits - 1), 1<<(bits-1) - 1
	}
	if bits == 64 {
		return false, 0, math.MaxUint64
	}
	return false, 0, 1<<bits - 1
}

// isNegative reports whether v < 0.
func isNegative[F Integer](v F) bool { return v < 0 }

// fits reports whether v lies in T's range.
func fits[T, F Integer](v F) bool {
	signed, minVal, maxVal := bounds[T]()
	if isNegative(v) {
		return signed && int64(v) >= minVal
	}
	return uint64(v) <= maxVal
}

// Saturate converts v to T, clamping to T's range instead of wrapping. Use it
// for values that are bounded by construction (a length, an index, a block
// height): it documents the conversion and can never wrap. For input an
// attacker controls, prefer Checked and refuse the message.
func Saturate[T, F Integer](v F) T {
	if fits[T](v) {
		return T(v)
	}
	signed, minVal, maxVal := bounds[T]()
	if isNegative(v) {
		if signed {
			return T(minVal)
		}
		return 0
	}
	return T(maxVal)
}

// Checked converts v to T, or returns an error when v is outside T's range.
func Checked[T, F Integer](v F) (T, error) {
	if !fits[T](v) {
		var t T
		return 0, fmt.Errorf("integer %d overflows %T", v, t)
	}
	return T(v), nil
}
