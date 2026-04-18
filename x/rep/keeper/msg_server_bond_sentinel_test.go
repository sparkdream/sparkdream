package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// seedSentinelCandidate creates a member with enough reputation to bond as a
// sentinel (tier >= DefaultMinRepTierSentinel) and enough unlocked DREAM to
// cover a bond.
func seedSentinelCandidate(t *testing.T, f *fixture, addr sdk.AccAddress, balance math.Int) {
	t.Helper()
	zero := math.ZeroInt()
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   &balance,
		StakedDream:    &zero,
		LifetimeEarned: &zero,
		LifetimeBurned: &zero,
		// Total reputation 250 puts the member in tier 3 (minimum for sentinels).
		ReputationScores: map[string]string{"backend": "250.0"},
	}))
}

func TestBondSentinel_HappyPath(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelAAA"))
	seedSentinelCandidate(t, f, addr, math.NewInt(5_000))

	_, err := srv.BondSentinel(f.ctx, &types.MsgBondSentinel{
		Creator: addr.String(),
		Amount:  "2000",
	})
	require.NoError(t, err)

	sa, err := f.keeper.GetSentinel(f.ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, "2000", sa.CurrentBond)
	require.Equal(t, types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL, sa.BondStatus)

	// DREAM was locked (staked goes up, spendable balance down).
	mem, err := f.keeper.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, "2000", mem.StakedDream.String())
}

func TestBondSentinel_RejectsLowReputation(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("novice"))
	// Give them DREAM but keep reputation below tier 3.
	zero := math.ZeroInt()
	balance := math.NewInt(5_000)
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:          addr.String(),
		DreamBalance:     &balance,
		StakedDream:      &zero,
		LifetimeEarned:   &zero,
		LifetimeBurned:   &zero,
		ReputationScores: map[string]string{"backend": "5.0"}, // tier 0
	}))

	_, err := srv.BondSentinel(f.ctx, &types.MsgBondSentinel{
		Creator: addr.String(),
		Amount:  "2000",
	})
	require.ErrorIs(t, err, types.ErrInsufficientReputation)
}

func TestBondSentinel_RejectsSubMinimum(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("tightwad"))
	seedSentinelCandidate(t, f, addr, math.NewInt(5_000))

	_, err := srv.BondSentinel(f.ctx, &types.MsgBondSentinel{
		Creator: addr.String(),
		Amount:  "10", // below DefaultMinSentinelBondAmount
	})
	require.ErrorIs(t, err, types.ErrBondAmountTooSmall)
}

func TestBondSentinel_RejectsDuringCooldown(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("cooldown"))
	seedSentinelCandidate(t, f, addr, math.NewInt(5_000))

	// Manually park the sentinel in cooldown.
	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, addr.String(), types.SentinelActivity{
		Address:               addr.String(),
		CurrentBond:           "0",
		BondStatus:            types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED,
		DemotionCooldownUntil: sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix() + 10_000,
	}))

	_, err := srv.BondSentinel(f.ctx, &types.MsgBondSentinel{
		Creator: addr.String(),
		Amount:  "2000",
	})
	require.ErrorIs(t, err, types.ErrDemotionCooldown)
}

func TestBondSentinel_InvalidAddress(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	_, err := srv.BondSentinel(f.ctx, &types.MsgBondSentinel{
		Creator: "not-an-address",
		Amount:  "2000",
	})
	require.Error(t, err)
}
