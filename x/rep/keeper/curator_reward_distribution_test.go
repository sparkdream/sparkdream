package keeper_test

import (
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// The curator role used to be pure downside: slashable bond, nothing earned for
// rating, and on winning a challenge only your own bond back. These tests pin
// the shape of the SPARK pay that closed that gap -- and in particular that it
// pays for rating *accurately*, not for rating *often*.

func bondCurator(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, bond int64) {
	t.Helper()
	require.NoError(t, k.BondedRoles.Set(ctx,
		collections.Join(int32(types.RoleType_ROLE_TYPE_COLLECT_CURATOR), addr.String()),
		types.BondedRole{
			Address:            addr.String(),
			RoleType:           types.RoleType_ROLE_TYPE_COLLECT_CURATOR,
			BondStatus:         types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
			CurrentBond:        math.NewInt(bond).String(),
			TotalCommittedBond: math.ZeroInt().String(),
		}))
}

// mkScoredCurator bonds an address as a NORMAL curator with a windowed record
// of upheld/overturned ratings, as x/collect reports them on challenge
// resolution.
func mkScoredCurator(t *testing.T, f *fixture, addr sdk.AccAddress, upheld, overturned int) {
	t.Helper()
	bondCurator(t, f.keeper, f.ctx, addr, 100_000_000)
	for i := 0; i < upheld; i++ {
		require.NoError(t, f.keeper.RecordRoleOutcome(f.ctx,
			types.RoleType_ROLE_TYPE_COLLECT_CURATOR, addr.String(), types.ActionKindCollectCuration, true))
	}
	for i := 0; i < overturned; i++ {
		require.NoError(t, f.keeper.RecordRoleOutcome(f.ctx,
			types.RoleType_ROLE_TYPE_COLLECT_CURATOR, addr.String(), types.ActionKindCollectCuration, false))
	}
}

func fundCuratorPool(f *fixture, ledger map[string]math.Int, amount math.Int) {
	ledger[keeper.CuratorRewardPoolAddress().String()] = amount
}

func curatorEpochCtx(t *testing.T, f *fixture) sdk.Context {
	t.Helper()
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	ctx := f.ctx.WithBlockHeight(int64(params.CuratorRewardEpochBlocks))
	require.True(t, f.keeper.IsCuratorRewardEpoch(ctx))
	return ctx
}

func TestCuratorPoolPaysOnlyAboveTheAccuracyBar(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	accurate := sdk.AccAddress([]byte("cur-accurate----"))
	sloppy := sdk.AccAddress([]byte("cur-sloppy------"))
	mkReviewMember(t, k, f.ctx, accurate, "500.0")
	mkReviewMember(t, k, f.ctx, sloppy, "500.0")
	mkScoredCurator(t, f, accurate, 9, 1) // 0.90
	mkScoredCurator(t, f, sloppy, 1, 9)   // 0.10

	ledger := installLedger(f)
	fundCuratorPool(f, ledger, math.NewInt(1_000_000))
	ctx := curatorEpochCtx(t, f)

	require.NoError(t, k.DistributeCuratorRewards(ctx))

	denom := k.BondDenom(ctx)
	require.True(t, f.bankKeeper.GetBalance(ctx, accurate, denom).Amount.IsPositive())
	require.True(t, f.bankKeeper.GetBalance(ctx, sloppy, denom).Amount.IsZero(),
		"a curator below min_curator_accuracy earns nothing")
}

func TestUncontestedCuratorEarnsNothing(t *testing.T) {
	// Rating volume alone must not pay, or the cheapest strategy is to rate
	// everything without looking -- which is exactly the failure the bond and
	// the challenge mechanism exist to deter.
	f := initFixture(t)
	k := f.keeper

	quiet := sdk.AccAddress([]byte("cur-quiet-------"))
	mkReviewMember(t, k, f.ctx, quiet, "500.0")
	mkScoredCurator(t, f, quiet, 0, 0)

	ledger := installLedger(f)
	fundCuratorPool(f, ledger, math.NewInt(1_000_000))
	ctx := curatorEpochCtx(t, f)

	require.NoError(t, k.DistributeCuratorRewards(ctx))
	require.True(t, f.bankKeeper.GetBalance(ctx, quiet, k.BondDenom(ctx)).Amount.IsZero())
	require.Equal(t, math.NewInt(1_000_000), k.GetCuratorRewardPool(ctx),
		"with nobody eligible the pool is left intact")
}

func TestCuratorPoolIsSeparateFromSentinelAndReviewer(t *testing.T) {
	// Three separate pools so no role can draw on another's funds or be
	// retuned by another's accuracy bar.
	f := initFixture(t)
	ledger := installLedger(f)
	fundCuratorPool(f, ledger, math.NewInt(750_000))

	require.Equal(t, math.NewInt(750_000), f.keeper.GetCuratorRewardPool(f.ctx))
	require.True(t, f.keeper.GetSentinelRewardPool(f.ctx).IsZero())
	require.True(t, f.keeper.GetReviewerRewardPool(f.ctx).IsZero())
}

func TestCuratorPoolOverflowIsBurned(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	cap := params.MaxCuratorRewardPool
	ledger := installLedger(f)
	fundCuratorPool(f, ledger, cap.MulRaw(2))

	require.NoError(t, k.BurnCuratorRewardPoolOverflow(f.ctx))

	expected := cap.MulRaw(2).Sub(cap.Quo(math.NewInt(2)))
	require.Equal(t, expected.String(), k.GetCuratorRewardPool(f.ctx).String())
}

func TestCuratorAccuracyRingUsesTheCuratorCadence(t *testing.T) {
	// Same guard the reviewer pool needed: the ring is stamped when a rating is
	// resolved and read as a window at distribution time, so both must be in
	// curator-epoch units. The three cadences are independently editable.
	f := initFixture(t)
	k := f.keeper

	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	params.SentinelRewardEpochBlocks = 400
	params.CuratorRewardEpochBlocks = 100
	require.NoError(t, k.Params.Set(f.ctx, params))

	curator := sdk.AccAddress([]byte("cur-cadence-----"))
	mkReviewMember(t, k, f.ctx, curator, "500.0")

	recordCtx := f.ctx.WithBlockHeight(1000)
	bondCurator(t, k, recordCtx, curator, 100_000_000)
	for i := 0; i < 9; i++ {
		require.NoError(t, k.RecordRoleOutcome(recordCtx,
			types.RoleType_ROLE_TYPE_COLLECT_CURATOR, curator.String(), types.ActionKindCollectCuration, true))
	}
	require.NoError(t, k.RecordRoleOutcome(recordCtx,
		types.RoleType_ROLE_TYPE_COLLECT_CURATOR, curator.String(), types.ActionKindCollectCuration, false))

	upheld, overturned := k.GetRoleWindowedAccuracy(recordCtx,
		types.RoleType_ROLE_TYPE_COLLECT_CURATOR, curator.String(),
		k.CurrentCuratorRewardEpoch(recordCtx), params.CuratorAccuracyWindowEpochs)
	require.Equal(t, uint64(9), upheld, "ratings must be visible in the curator's own epoch window")
	require.Equal(t, uint64(1), overturned)
}

func TestCuratorRewardParamValidation(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	require.Equal(t, p.MaxSentinelRewardPool, p.MaxCuratorRewardPool,
		"curator and sentinel pools are sized equal by decision")
	require.True(t, p.MaxReviewerRewardPool.GT(p.MaxCuratorRewardPool),
		"reviewers are paid more than sentinels and curators")

	p.MinCuratorAccuracy = math.LegacyNewDec(2)
	require.Error(t, p.Validate())

	p = types.DefaultParams()
	p.CuratorRewardEpochBlocks = 0
	require.Error(t, p.Validate(), "a zero cadence would divide by zero")

	op := types.DefaultRepOperationalParams()
	require.Equal(t, types.DefaultParams().MaxCuratorRewardPool, op.MaxCuratorRewardPool)
	op.CuratorAccuracyWindowEpochs = 0
	require.Error(t, op.Validate())
}
