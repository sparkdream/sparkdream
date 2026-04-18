package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/types"
)

func TestKeeper_SetParams(t *testing.T) {
	f := initFixture(t)

	// Default params should already be set by initFixture.
	got, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.DefaultParams(), got)

	// Override with a custom set and confirm round-trip through SetParams.
	custom := types.DefaultParams()
	custom.MaxNameLength = 42
	custom.RegistrationFee = sdk.NewCoin("uspark", math.NewInt(7))
	custom.ExpirationDuration = 2 * time.Hour

	require.NoError(t, f.keeper.SetParams(f.ctx, custom))

	got, err = f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, custom, got)
}
