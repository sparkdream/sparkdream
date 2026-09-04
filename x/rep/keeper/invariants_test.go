package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// x/rep had no registered invariants at all, so none of its failure classes was
// detectable on-chain by x/crisis. Each test here drives its invariant both
// ways: clean state must pass, and the specific corruption it exists to catch
// must be reported.

func TestSeasonalPoolDenominatorInvariant(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	ctx := sdk.UnwrapSDKContext(f.ctx)

	creator := newStakerMember(t, f, "inv_pool_creator____", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "inv_pool_staker_____", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "invpool")

	_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	inv := keeper.SeasonalPoolDenominatorInvariant(k)
	msg, broken := inv(ctx)
	require.False(t, broken, "a pool maintained through updateStakePoolTotals must hold: %s", msg)

	// Drift the denominator the way a mutation bypassing updateStakePoolTotals
	// would: every seasonal payout divides by this.
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(f.ctx, math.NewInt(500_000)))
	msg, broken = inv(ctx)
	require.True(t, broken, "a denominator above the live stakes must be reported")
	require.Contains(t, msg, "total_staked")
}

func TestMemberStakedWithinBalanceInvariant(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	ctx := sdk.UnwrapSDKContext(f.ctx)

	addr := newStakerMember(t, f, "inv_balance_member__", math.NewInt(1_000_000))

	inv := keeper.MemberStakedWithinBalanceInvariant(k)
	msg, broken := inv(ctx)
	require.False(t, broken, "a freshly funded member must hold: %s", msg)

	// staked_dream is a subset of dream_balance; inverting the relation is what
	// silently corrupts `unlocked = balance - staked` everywhere it is read.
	m, err := k.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	m.StakedDream = PtrInt(keeper.DerefInt(m.DreamBalance).AddRaw(1))
	require.NoError(t, k.Member.Set(f.ctx, addr.String(), m))

	msg, broken = inv(ctx)
	require.True(t, broken, "staked_dream above dream_balance must be reported")
	require.Contains(t, msg, "exceeding dream_balance")
}

func TestTreasuryNonNegativeInvariant(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := sdk.UnwrapSDKContext(f.ctx)

	inv := keeper.TreasuryNonNegativeInvariant(k)
	msg, broken := inv(ctx)
	require.False(t, broken, "a fresh treasury must hold: %s", msg)

	require.NoError(t, k.TreasuryBalance.Set(f.ctx, "-1"))
	msg, broken = inv(ctx)
	require.True(t, broken, "a negative treasury must be reported")
	require.Contains(t, msg, "negative")
}

func TestSeasonCapsNotExceededInvariant(t *testing.T) {
	params := types.DefaultParams()
	params.MaxInterimRewardsPerSeason = math.NewInt(1_000)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := sdk.UnwrapSDKContext(f.ctx)

	inv := keeper.SeasonCapsNotExceededInvariant(k)
	msg, broken := inv(ctx)
	require.False(t, broken, "zero counters must hold: %s", msg)

	// A counter above its cap means a payout path minted without charging the
	// gate — which is how the interim path behaved before it had a cap.
	require.NoError(t, k.TrackInterimRewardMint(f.ctx, math.NewInt(5_000)))
	msg, broken = inv(ctx)
	require.False(t, broken,
		"cap breaches are warning-grade: a governance cap cut mid-season must not halt the chain")
	require.Contains(t, msg, "exceed the cap")
}
