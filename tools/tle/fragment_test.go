package tle

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDecodeExecFragmentMalformed feeds parseFields every truncation and
// wire-type error it guards: each must return an error, never panic.
func TestDecodeExecFragmentMalformed(t *testing.T) {
	// A valid inner Any (type_url only) to wrap in field 2.
	anyBytes := appendBytesField(nil, 1, []byte("/a.B"))
	wrap := func(inner []byte) []byte { return appendBytesField(nil, 2, inner) }

	tests := []struct {
		name    string
		in      []byte
		wantErr string
	}{
		{"unterminated tag varint", []byte{0x80}, "bad tag"},
		{"tag varint longer than 10 bytes", []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, "bad tag"},
		{"varint field with no value", []byte{0x08}, "bad varint"},
		{"varint field with unterminated value", []byte{0x08, 0xff}, "bad varint"},
		{"fixed64 field short", []byte{0x09, 1, 2, 3, 4, 5, 6, 7}, "short fixed64"},
		{"fixed32 field short", []byte{0x0d, 1, 2, 3}, "short fixed32"},
		{"length-delimited with no length", []byte{0x12}, "bad length"},
		{"length beyond buffer", []byte{0x12, 0x05, 1, 2}, "bad length"},
		{"huge length", []byte{0x12, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}, "bad length"},
		{"start-group wire type", []byte{0x13}, "unsupported wire type 3"},
		{"end-group wire type", []byte{0x14}, "unsupported wire type 4"},
		{"wire type 6", []byte{0x16}, "unsupported wire type 6"},
		{"wire type 7", []byte{0x17}, "unsupported wire type 7"},
		{"empty fragment has no inner_message", nil, "no inner_message"},
		{"only proof and nullifier", appendBytesField(appendBytesField(nil, 3, []byte("p")), 4, []byte("n")), "no inner_message"},
		{"malformed inner Any", wrap([]byte{0x0a, 0x09, 'x'}), "inner_message: tle: bad length"},
		{"inner Any with group wire type", wrap(append(append([]byte(nil), anyBytes...), 0x0b)), "inner_message: tle: unsupported wire type 3"},
		{"trailing garbage after valid field", append(wrap(anyBytes), 0x80), "bad tag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, _, _, _, err := DecodeExecFragment(tt.in)
				require.ErrorContains(t, err, tt.wantErr)
			})
		})
	}
}

// TestDecodeExecFragmentSkipsScalarFields: varint and fixed fields are
// skipped, and the last occurrence of a length-delimited field wins.
func TestDecodeExecFragmentSkipsScalarFields(t *testing.T) {
	bz := []byte{0x08, 0x96, 0x01}                // field 1 varint 150
	bz = append(bz, 0x29, 1, 2, 3, 4, 5, 6, 7, 8) // field 5 fixed64
	bz = append(bz, 0x35, 1, 2, 3, 4)             // field 6 fixed32
	bz = appendBytesField(bz, 2, appendBytesField(nil, 1, []byte("/old.Msg")))
	bz = appendBytesField(bz, 2, appendBytesField(appendBytesField(nil, 1, []byte("/new.Msg")), 2, []byte{9}))
	bz = appendBytesField(bz, 3, []byte("proof"))

	url, val, proof, null, err := DecodeExecFragment(bz)
	require.NoError(t, err)
	require.Equal(t, "/new.Msg", url)
	require.Equal(t, []byte{9}, val)
	require.Equal(t, []byte("proof"), proof)
	require.Nil(t, null)
}
