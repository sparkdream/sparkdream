package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"
)

func TestParamsMediaDefaultsAndBounds(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, commontypes.DefaultMediaMinTrustLevel, p.MediaMinTrustLevel)
	require.True(t, p.MediaAuthorBondMin.Equal(commontypes.DefaultMediaAuthorBondMin))
	require.True(t, p.MediaScanFee.IsZero())
	require.NoError(t, p.Validate())

	bad := p
	bad.MediaMinTrustLevel = 5
	require.Error(t, bad.Validate())
	bad = p
	bad.MediaAuthorBondMin = math.NewInt(-1)
	require.Error(t, bad.Validate())
	bad = p
	bad.MediaScanFee = math.NewInt(-1)
	require.Error(t, bad.Validate())
}
