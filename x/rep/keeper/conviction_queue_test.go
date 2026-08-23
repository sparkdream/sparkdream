package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// queueEntries returns every (dueAt, initiativeID) pair currently queued.
func queueEntries(t *testing.T, f *fixture, ctx sdk.Context) []collections.Pair[int64, uint64] {
	t.Helper()
	var out []collections.Pair[int64, uint64]
	require.NoError(t, f.keeper.ConvictionQueue.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		out = append(out, key)
		return false, nil
	}))
	return out
}

func scheduledAt(t *testing.T, f *fixture, ctx sdk.Context, initiativeID uint64) (int64, bool) {
	t.Helper()
	due, err := f.keeper.ConvictionScheduledAt.Get(ctx, initiativeID)
	if err != nil {
		return 0, false
	}
	return due, true
}

// TestConvictionQueue_StakeArmsFutureRefresh asserts that staking recomputes
// conviction immediately (so the staker sees it) and arms the initiative for a
// later refresh rather than leaving it due — otherwise the next EndBlocker would
// redo work that is already current.
func TestConvictionQueue_StakeArmsFutureRefresh(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "cq_arm_creator______", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "cq_arm_staker_______", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "arm")

	_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	now := sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix()
	due, ok := scheduledAt(t, f, sdk.UnwrapSDKContext(f.ctx), initID)
	require.True(t, ok, "staking must put the initiative on the conviction queue")
	require.Greater(t, due, now, "a freshly recomputed initiative should be armed for the future, not left due")

	// Conviction is already current, so an immediate drain is a no-op that
	// leaves the schedule untouched.
	require.NoError(t, k.DrainConvictionQueue(f.ctx))
	after, ok := scheduledAt(t, f, sdk.UnwrapSDKContext(f.ctx), initID)
	require.True(t, ok)
	require.Equal(t, due, after)
}

// TestConvictionQueue_DrainProcessesDueAndReschedules walks the queue forward in
// time and asserts the entry is consumed and re-armed rather than lost.
func TestConvictionQueue_DrainProcessesDueAndReschedules(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "cq_drain_creator____", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "cq_drain_staker_____", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "drain")

	_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	firstDue, ok := scheduledAt(t, f, sdk.UnwrapSDKContext(f.ctx), initID)
	require.True(t, ok)

	// Advance past the scheduled time so the entry becomes due.
	ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(firstDue+1, 0))
	require.NoError(t, k.DrainConvictionQueue(ctx))

	secondDue, ok := scheduledAt(t, f, ctx, initID)
	require.True(t, ok, "a processed initiative must stay armed, not fall off the queue")
	require.Greater(t, secondDue, firstDue, "processing must push the next refresh forward")

	// Exactly one entry — rescheduling replaces, it does not duplicate.
	require.Len(t, queueEntries(t, f, ctx), 1)
}

// TestConvictionQueue_MaturedInitiativeGetsLongCadence is the heart of the
// incremental design: once every stake has matured, conviction stops changing on
// its own, so the initiative drops to a slow cadence and stops costing per-block
// work. This is what removes dust stakes from the cost model.
func TestConvictionQueue_MaturedInitiativeGetsLongCadence(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "cq_mature_creator___", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "cq_mature_staker____", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "mature")

	_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	halfLife := int64(params.ConvictionHalfLifeEpochs * params.EpochBlocks * 6)

	// While maturing, the cadence is a fraction of the half-life.
	maturingDue, ok := scheduledAt(t, f, sdk.UnwrapSDKContext(f.ctx), initID)
	require.True(t, ok)
	now := sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix()
	require.Less(t, maturingDue-now, halfLife,
		"a maturing initiative should refresh well inside one half-life")

	// Jump past full maturity (2 * halfLife) and drain.
	matureTime := now + 2*halfLife + 1
	ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(matureTime, 0))
	require.NoError(t, k.DrainConvictionQueue(ctx))

	stableDue, ok := scheduledAt(t, f, ctx, initID)
	require.True(t, ok)
	require.Equal(t, matureTime+keeper.ConvictionStableRefreshSeconds, stableDue,
		"a fully matured initiative should drop to the stable refresh cadence")
}

// TestConvictionQueue_ContentStakeReschedulesLinkedInitiative covers the reverse
// index. Content conviction propagates into linked initiatives, so an
// incremental refresh that only watched initiative stakes would silently stop
// tracking it.
func TestConvictionQueue_ContentStakeReschedulesLinkedInitiative(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "cq_content_creator__", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "cq_content_staker___", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "content")

	const contentID uint64 = 4242
	require.NoError(t, k.RegisterContentInitiativeLink(
		f.ctx, initID, int32(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT), contentID))

	// Push the initiative out to a far-future refresh so a change is visible.
	future := sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix() + 1_000_000
	require.NoError(t, k.ScheduleConvictionRefresh(f.ctx, initID, future))

	_, err := k.CreateStake(
		f.ctx, staker, types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, contentID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	now := sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix()
	due, ok := scheduledAt(t, f, sdk.UnwrapSDKContext(f.ctx), initID)
	require.True(t, ok)
	require.Equal(t, now, due,
		"staking on linked content must pull the initiative's refresh forward to now")

	// And unlinking removes it from the reverse index, so later content stakes
	// no longer reach this initiative.
	require.NoError(t, k.RemoveContentInitiativeLink(
		f.ctx, initID, int32(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT), contentID))
	require.NoError(t, k.ScheduleConvictionRefresh(f.ctx, initID, future))

	_, err = k.CreateStake(
		f.ctx, staker, types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, contentID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	due, ok = scheduledAt(t, f, sdk.UnwrapSDKContext(f.ctx), initID)
	require.True(t, ok)
	require.Equal(t, future, due, "an unlinked initiative should no longer be reached by content stakes")
}

// TestConvictionQueue_TerminalInitiativeLeavesQueue asserts an initiative that
// can no longer accrue is dropped, so completed work does not accumulate in the
// queue forever.
func TestConvictionQueue_TerminalInitiativeLeavesQueue(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "cq_term_creator_____", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "cq_term_staker______", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "term")

	_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)
	_, ok := scheduledAt(t, f, sdk.UnwrapSDKContext(f.ctx), initID)
	require.True(t, ok)

	// Drive it to a terminal status.
	initiative, err := k.GetInitiative(f.ctx, initID)
	require.NoError(t, err)
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED
	require.NoError(t, k.UpdateInitiative(f.ctx, initiative))

	due, _ := scheduledAt(t, f, sdk.UnwrapSDKContext(f.ctx), initID)
	ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(due+1, 0))
	require.NoError(t, k.DrainConvictionQueue(ctx))

	_, ok = scheduledAt(t, f, ctx, initID)
	require.False(t, ok, "a completed initiative must be dropped from the conviction queue")
	require.Empty(t, queueEntries(t, f, ctx))
}

// TestConvictionQueue_PerBlockBudgetRollsOver is the liveness property the user
// asked for: a backlog larger than one block's budget is processed across
// several blocks instead of inflating a single block.
func TestConvictionQueue_PerBlockBudgetRollsOver(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "cq_budget_creator___", math.NewInt(50_000_000_000))

	projectID, err := k.CreateProject(
		f.ctx, creator, "BudgetProj", "Desc", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100_000_000), math.NewInt(1_000_000), false,
	)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(100_000_000), math.NewInt(1_000_000)))

	// One more initiative than a single block's budget allows. Each is
	// stake-less, so each charges exactly one unit of budget.
	total := keeper.MaxConvictionStakeUpdatesPerBlock + 25
	for i := 0; i < total; i++ {
		_, err := k.CreateInitiative(
			f.ctx, creator, projectID, "T", "D", []string{"tag1"},
			types.InitiativeTier_INITIATIVE_TIER_APPRENTICE,
			types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1_000),
		)
		require.NoError(t, err)
	}

	require.NoError(t, k.RearmConvictionQueue(f.ctx))
	queued := len(queueEntries(t, f, sdk.UnwrapSDKContext(f.ctx)))
	require.GreaterOrEqual(t, queued, total)

	// All entries are due now. One drain must leave a remainder behind.
	require.NoError(t, k.DrainConvictionQueue(f.ctx))

	now := sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix()
	stillDue := 0
	for _, key := range queueEntries(t, f, sdk.UnwrapSDKContext(f.ctx)) {
		if key.K1() <= now {
			stillDue++
		}
	}
	require.Positive(t, stillDue, "work beyond the per-block budget must roll over to the next block")
	require.LessOrEqual(t, queued-stillDue, keeper.MaxConvictionStakeUpdatesPerBlock+1,
		"a single block must not exceed its conviction work budget")

	// A second block clears the remainder.
	require.NoError(t, k.DrainConvictionQueue(f.ctx))
	for _, key := range queueEntries(t, f, sdk.UnwrapSDKContext(f.ctx)) {
		require.Greater(t, key.K1(), now, "the backlog should be drained after enough blocks")
	}
}
