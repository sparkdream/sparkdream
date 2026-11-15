package vision

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
)

// tensor is one float32 tensor read from a safetensors file.
type tensor struct {
	shape []int
	data  []float32
}

// maxHeaderBytes bounds the JSON header of a weights file.
const maxHeaderBytes = 16 << 20

// sizeOf converts a length, count or index to uint64. They are never
// negative; the guard keeps the conversion from wrapping if one ever were.
func sizeOf(n int) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n)
}

// parseSafetensors reads every F32 tensor from a safetensors file: an 8-byte
// little-endian header length, a JSON header of name -> {dtype, shape,
// data_offsets}, then the raw little-endian data.
func parseSafetensors(raw []byte) (map[string]tensor, error) {
	if len(raw) < 8 {
		return nil, fmt.Errorf("vision: weights file is too short")
	}
	n := binary.LittleEndian.Uint64(raw[:8])
	if n > maxHeaderBytes || n > sizeOf(len(raw)-8) {
		return nil, fmt.Errorf("vision: weights file has a bad header length")
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(raw[8:8+n], &header); err != nil {
		return nil, fmt.Errorf("vision: weights header: %w", err)
	}
	body := raw[8+n:]
	out := map[string]tensor{}
	for name, rawEntry := range header {
		if name == "__metadata__" {
			continue
		}
		var e struct {
			DType   string    `json:"dtype"`
			Shape   []int     `json:"shape"`
			Offsets [2]uint64 `json:"data_offsets"`
		}
		if err := json.Unmarshal(rawEntry, &e); err != nil {
			return nil, fmt.Errorf("vision: weights header entry %q: %w", name, err)
		}
		if e.DType != "F32" {
			return nil, fmt.Errorf("vision: tensor %q is %s, want F32", name, e.DType)
		}
		count := 1
		for _, d := range e.Shape {
			if d < 0 || (d > 0 && count > math.MaxInt32/d) {
				return nil, fmt.Errorf("vision: tensor %q has a bad shape", name)
			}
			count *= d
		}
		lo, hi := e.Offsets[0], e.Offsets[1]
		if lo > hi || hi > sizeOf(len(body)) || hi-lo != sizeOf(count)*4 {
			return nil, fmt.Errorf("vision: tensor %q has bad data offsets", name)
		}
		data := make([]float32, count)
		for i := range data {
			data[i] = math.Float32frombits(binary.LittleEndian.Uint32(body[lo+sizeOf(i)*4:]))
		}
		out[name] = tensor{shape: e.Shape, data: data}
	}
	return out, nil
}
