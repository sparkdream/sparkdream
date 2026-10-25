package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vault/types"
)

func TestParamsValidate(t *testing.T) {
	p := types.DefaultParams()
	require.True(t, p.CreationEnabled)
	require.Equal(t, math.NewInt(10_000_000), p.CreationFeeAmount, "10 SPARK in micro-units")
	require.NoError(t, p.Validate())

	zero := types.NewParams(false, math.ZeroInt())
	require.NoError(t, zero.Validate(), "a zero fee and disabled creation are valid")

	neg := types.NewParams(true, math.NewInt(-1))
	require.Error(t, neg.Validate())

	var nilInt types.Params
	nilInt.CreationEnabled = true
	require.Error(t, nilInt.Validate(), "a nil amount is refused")
}
