package types_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/common/types"
)

func TestSaturate(t *testing.T) {
	require.Equal(t, uint32(7), types.Saturate[uint32](7))
	require.Equal(t, uint32(math.MaxUint32), types.Saturate[uint32](int64(math.MaxUint32)+5))
	require.Equal(t, uint32(0), types.Saturate[uint32](-3))
	require.Equal(t, uint64(0), types.Saturate[uint64](int64(-1)))
	require.Equal(t, int64(math.MaxInt64), types.Saturate[int64](uint64(math.MaxUint64)))
	require.Equal(t, int64(math.MaxInt64), types.Saturate[int64](uint64(math.MaxInt64)))
	require.Equal(t, int32(math.MinInt32), types.Saturate[int32](int64(math.MinInt64)))
	require.Equal(t, int8(-5), types.Saturate[int8](-5))
	require.Equal(t, uint8(255), types.Saturate[uint8](1000))
	require.Equal(t, int(math.MaxInt), types.Saturate[int](uint64(math.MaxUint64)))
}

func TestChecked(t *testing.T) {
	v, err := types.Checked[uint32](42)
	require.NoError(t, err)
	require.Equal(t, uint32(42), v)
	_, err = types.Checked[uint32](-1)
	require.Error(t, err)
	_, err = types.Checked[int64](uint64(math.MaxUint64))
	require.Error(t, err)
	_, err = types.Checked[uint16](70000)
	require.Error(t, err)
	w, err := types.Checked[int64](uint64(math.MaxInt64))
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64), w)
	n, err := types.Checked[int8](-128)
	require.NoError(t, err)
	require.Equal(t, int8(-128), n)
}
