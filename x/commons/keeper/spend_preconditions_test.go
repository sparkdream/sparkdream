package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/types"
)

// TestCheckSpendPreconditions_HappyPath verifies the helper passes and
// updates the per-epoch counter for a normal spend.
func TestCheckSpendPreconditions_HappyPath(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("pre_happy_council___"))
	limit := math.NewInt(1000)
	now := ctx.BlockTime().Unix()
	require.NoError(t, k.Groups.Set(ctx, "Pre", types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &limit,
		CurrentTermExpiration: now + 3600,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, council.String(), "Pre"))

	amt := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(400)))
	require.NoError(t, k.CheckSpendPreconditions(ctx, council.String(), amt))

	// A second 400-uspark spend in the same epoch still fits — total 800/1000.
	require.NoError(t, k.CheckSpendPreconditions(ctx, council.String(), amt))

	// A third 400-uspark spend tips over the ceiling — total would be 1200/1000.
	err := k.CheckSpendPreconditions(ctx, council.String(), amt)
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)
}

// TestCheckSpendPreconditions_UnknownAuthority — the helper must reject
// callers whose policy address is not registered, mirroring MsgSpendFromCommons.
func TestCheckSpendPreconditions_UnknownAuthority(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	stranger := sdk.AccAddress([]byte("pre_stranger________"))
	err := k.CheckSpendPreconditions(ctx, stranger.String(),
		sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1))))
	require.ErrorIs(t, err, types.ErrGroupNotFound)
}

// TestCheckSpendPreconditions_ShellAndZombie — both pre-launch and expired
// groups must reject. Same gate as MsgSpendFromCommons; verifying the
// helper directly catches regressions where one path drifts from the other.
func TestCheckSpendPreconditions_ShellAndZombie(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))
	now := ctx.BlockTime().Unix()

	shell := sdk.AccAddress([]byte("pre_shell___________"))
	require.NoError(t, k.Groups.Set(ctx, "Shell", types.Group{
		PolicyAddress:  shell.String(),
		ActivationTime: now + 3600,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, shell.String(), "Shell"))

	zombie := sdk.AccAddress([]byte("pre_zombie__________"))
	require.NoError(t, k.Groups.Set(ctx, "Zombie", types.Group{
		PolicyAddress:         zombie.String(),
		CurrentTermExpiration: now - 3600,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, zombie.String(), "Zombie"))

	require.ErrorIs(t,
		k.CheckSpendPreconditions(ctx, shell.String(),
			sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1)))),
		types.ErrGroupNotActive,
	)
	require.ErrorIs(t,
		k.CheckSpendPreconditions(ctx, zombie.String(),
			sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1)))),
		types.ErrGroupExpired,
	)
}

// TestCheckSpendPreconditions_NoLimitGroup — a group with MaxSpendPerEpoch=0
// (or nil) should pass arbitrary amounts; the rate limit is opt-in via the
// configured ceiling, not always-on.
func TestCheckSpendPreconditions_NoLimitGroup(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("pre_nolimit_________"))
	zero := math.ZeroInt()
	now := ctx.BlockTime().Unix()
	require.NoError(t, k.Groups.Set(ctx, "NoLimit", types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &zero,
		CurrentTermExpiration: now + 3600,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, council.String(), "NoLimit"))

	bigAmt := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000_000_000_000)))
	require.NoError(t, k.CheckSpendPreconditions(ctx, council.String(), bigAmt))
}

// TestCheckSpendPreconditions_EpochRollover — the counter is keyed on UTC
// day. Advancing block time past midnight resets the cumulative bucket.
func TestCheckSpendPreconditions_EpochRollover(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("pre_rollover________"))
	limit := math.NewInt(100)
	// Pin the start to a known epoch boundary.
	day0 := int64(86_400 * 1000)
	ctx = ctx.WithBlockTime(time.Unix(day0+10, 0))

	require.NoError(t, k.Groups.Set(ctx, "Roll", types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &limit,
		CurrentTermExpiration: 0, // never expires
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, council.String(), "Roll"))

	amt := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(80)))
	require.NoError(t, k.CheckSpendPreconditions(ctx, council.String(), amt))
	require.ErrorIs(t,
		k.CheckSpendPreconditions(ctx, council.String(), amt),
		types.ErrRateLimitExceeded,
	)

	// Advance one full day — the cumulative bucket for the new day starts fresh.
	ctx = ctx.WithBlockTime(time.Unix(day0+86_400+10, 0))
	require.NoError(t, k.CheckSpendPreconditions(ctx, council.String(), amt))
}

