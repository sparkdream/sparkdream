package keeper_test

import (
	"testing"

	"sparkdream/x/commons/types"

	"github.com/stretchr/testify/require"
)

func TestGetParams_Default_WhenUnset(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	// Fresh keeper has no stored params — should return defaults without error.
	got, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, types.DefaultParams(), got)
}

func TestSetParams_RoundTrip(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	custom := types.NewParams("1000uspark")
	require.NoError(t, k.SetParams(ctx, custom))

	got, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, custom, got)
}

func TestSetParams_RejectsInvalid(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	err := k.SetParams(ctx, types.NewParams("not-a-coin"))
	require.Error(t, err)
}
