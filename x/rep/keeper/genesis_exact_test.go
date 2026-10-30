package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestGenesis_RoundTripMatchesLiveState covers state the genesis round trip
// used to reshape:
//   - reserved tags export the live set only (DefaultGenesis seeds one);
//   - a jury review kept out of the verdict index (content challenge,
//     appeal) stays out after import, an indexed one stays in;
//   - the tag-budget award cooldown keeps its height;
//   - epoch-0 pool and mint epochs, stored with their partners, survive.
func TestGenesis_RoundTripMatchesLiveState(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	require.NoError(t, k.RemoveReservedTag(ctx, types.ReservedTagServiceOperator))
	require.NoError(t, k.SetReservedTag(ctx, types.ReservedTag{Name: "zenith", Authority: "gov"}))

	indexed := types.JuryReview{Id: 1, InitiativeId: 3, Verdict: types.Verdict_VERDICT_PENDING}
	outside := types.JuryReview{Id: 2, ContentChallengeId: 4, Verdict: types.Verdict_VERDICT_PENDING}
	require.NoError(t, k.JuryReview.Set(ctx, indexed.Id, indexed))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, indexed))
	require.NoError(t, k.JuryReview.Set(ctx, outside.Id, outside))

	require.NoError(t, k.TagBudgetAward.Set(ctx, 1, types.TagBudgetAward{Id: 1, BudgetId: 5, PostId: 6, Amount: "1"}))
	require.NoError(t, k.TagBudgetAwardByPost.Set(ctx, collections.Join(uint64(5), uint64(6)), int64(42)))

	require.NoError(t, k.SeasonalPoolRemaining.Set(ctx, "100"))
	require.NoError(t, k.SeasonalPoolSeason.Set(ctx, 0))
	require.NoError(t, k.SeasonalPoolStartEpoch.Set(ctx, 0))
	require.NoError(t, k.EpochMintedAmount.Set(ctx, "5"))
	require.NoError(t, k.EpochMintedEpoch.Set(ctx, 0))

	exported, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, []types.ReservedTag{{Name: "zenith", Authority: "gov"}}, exported.ReservedTagMap)
	require.Equal(t, []uint64{2}, exported.JuryReviewsOutsideVerdictIndex)
	require.Equal(t, []types.TagBudgetAwardCooldown{{BudgetId: 5, PostId: 6, Height: 42}}, exported.TagBudgetAwardCooldownList)

	// initFixture boots from DefaultGenesis, which reserves the tag; drop it
	// so the import starts from the exported set alone.
	g := initFixture(t)
	k2, ctx2 := g.keeper, g.ctx
	require.NoError(t, k2.RemoveReservedTag(ctx2, types.ReservedTagServiceOperator))
	require.NoError(t, k2.InitGenesis(ctx2, *exported))

	has, err := k2.ReservedTag.Has(ctx2, types.ReservedTagServiceOperator)
	require.NoError(t, err)
	require.False(t, has, "a removed default stays removed")

	inIndex := func(r types.JuryReview) bool {
		ok, err := k2.JuryReviewsByVerdict.Has(ctx2, collections.Join(int32(r.Verdict), r.Id))
		require.NoError(t, err)
		return ok
	}
	require.True(t, inIndex(indexed))
	require.False(t, inIndex(outside), "the deadline sweep must not reach a content review")

	h, err := k2.TagBudgetAwardByPost.Get(ctx2, collections.Join(uint64(5), uint64(6)))
	require.NoError(t, err)
	require.Equal(t, int64(42), h)

	for _, item := range []collections.Item[uint64]{k2.SeasonalPoolSeason, k2.SeasonalPoolStartEpoch, k2.EpochMintedEpoch} {
		has, err := item.Has(ctx2)
		require.NoError(t, err)
		require.True(t, has, "an epoch-0 value written with its partner is kept")
	}
}

// TestGenesis_RoundTripKeepsRewardDebtAndAwarderEpochs: the seasonal
// accumulator is exported, so an import must not rebase stake reward debts
// (that forfeited every pending seasonal reward), and the per-awarder epoch
// spend ledger travels with the genesis (an import used to reset each
// awarder's epoch cap).
func TestGenesis_RoundTripKeepsRewardDebtAndAwarderEpochs(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	require.NoError(t, k.SeasonalPoolAccPerShare.Set(ctx, "3.000000000000000000"))
	stake := types.Stake{Id: 7, Staker: "phoenix", TargetType: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		TargetId: 1, Amount: math.NewInt(100), RewardDebt: math.NewInt(120)}
	require.NoError(t, k.Stake.Set(ctx, stake.Id, stake))
	require.NoError(t, k.TagBudgetAwarderEpoch.Set(ctx, collections.Join3(uint64(5), "aurora", int64(9)), math.NewInt(250)))

	exported, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Len(t, exported.TagBudgetAwarderEpochList, 1)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))

	got, err := g.keeper.Stake.Get(g.ctx, stake.Id)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(120), got.RewardDebt, "pending seasonal reward (100*3-120) survives the import")
	amt, err := g.keeper.TagBudgetAwarderEpoch.Get(g.ctx, collections.Join3(uint64(5), "aurora", int64(9)))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(250), amt)
}
