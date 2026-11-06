package keeper_test

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/reveal/types"
)

// Deadline failure handling: transient x/rep refusals defer to the next
// epoch without counting (REV-201), real failures count and poison into a
// once-per-epoch cadence (REV-203), and any save clears the count
// (REV-202).

// countEvents counts the events of a type emitted on ctx.
func countEvents(ctx sdk.Context, typ string) int {
	n := 0
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

// revealedPastVerification builds a single-tranche contribution whose
// tranche has passed verification (two yes votes) and returns the fixture
// context positioned at its verification deadline.
func revealedPastVerification(t *testing.T, f *testFixture) (uint64, sdk.Context) {
	t.Helper()
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 500)
	f.stakeOnTranche(t, contribID, 0, f.staker2, 500)
	f.revealTranche(t, contribID, 0)
	params := testParams()
	params.MinVerificationVotes = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	f.castVerifyVote(t, contribID, 0, f.staker, true, 4)
	f.castVerifyVote(t, contribID, 0, f.staker2, true, 5)
	c, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	return contribID, f.sdkCtx.WithBlockHeight(c.Tranches[0].VerificationDeadline).WithEventManager(sdk.NewEventManager())
}

// TestREV201_MintCapIsTransientAndDefersToNextEpoch: a verified tranche
// whose payout mint is refused by the per-epoch cap is not counted as a
// failure and is retried at the next x/rep epoch boundary, where it pays.
func TestREV201_MintCapIsTransientAndDefersToNextEpoch(t *testing.T) {
	f := initTestFixture(t)
	contribID, ctx := revealedPastVerification(t, f)

	f.repKeeper.mintDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		return reptypes.ErrDreamMintCapExceeded
	}
	require.NoError(t, f.keeper.ProcessDeadlines(ctx))

	next, err := f.keeper.ContributionNextDeadline.Get(ctx, contribID)
	require.NoError(t, err)
	nextEpoch, err := f.repKeeper.NextEpochStartHeight(ctx)
	require.NoError(t, err)
	require.Equal(t, nextEpoch, next, "retried at the next x/rep epoch, not +100 blocks")
	_, err = f.keeper.DeadlineFailures.Get(ctx, contribID)
	require.ErrorIs(t, err, collections.ErrNotFound, "a transient refusal is not a failure")
	require.Equal(t, 1, countEvents(ctx, "contribution_deadline_failed"))
	require.Equal(t, 0, countEvents(ctx, "contribution_deadline_poisoned"))
	c, err := f.keeper.Contribution.Get(ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS, c.Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_REVEALED, c.Tranches[0].Status)

	// The cap stays exhausted for several epochs: still never counted.
	for i := 0; i < int(maxFailuresForTest)+1; i++ {
		ctx = ctx.WithBlockHeight(next).WithEventManager(sdk.NewEventManager())
		require.NoError(t, f.keeper.ProcessDeadlines(ctx))
		next, err = f.keeper.ContributionNextDeadline.Get(ctx, contribID)
		require.NoError(t, err)
		require.Equal(t, ctx.BlockHeight()+mockEpochBlocks, next)
		require.Equal(t, 0, countEvents(ctx, "contribution_deadline_poisoned"))
	}
	_, err = f.keeper.DeadlineFailures.Get(ctx, contribID)
	require.ErrorIs(t, err, collections.ErrNotFound)

	// The budget renews: the tranche pays and the contribution completes.
	paid := math.ZeroInt()
	f.repKeeper.mintDREAMFn = func(_ context.Context, _ sdk.AccAddress, amount math.Int) error {
		paid = paid.Add(amount)
		return nil
	}
	ctx = ctx.WithBlockHeight(next)
	require.NoError(t, f.keeper.ProcessDeadlines(ctx))
	c, err = f.keeper.Contribution.Get(ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED, c.Status)
	require.True(t, paid.IsPositive(), "the verified tranche's payout is minted once the cap renews")
	_, err = f.keeper.ContributionNextDeadline.Get(ctx, contribID)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

// maxFailuresForTest mirrors keeper.maxDeadlineFailures.
const maxFailuresForTest = uint32(3)

// TestREV203_RealFailuresPoisonIntoEpochCadence: three consecutive real
// failures emit contribution_deadline_poisoned once, keep the entry in the
// index at the next epoch boundary (never dropped), and the first
// successful step clears the count.
func TestREV203_RealFailuresPoisonIntoEpochCadence(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 300)
	c, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	f.repKeeper.unlockDREAMFn = func(_ context.Context, a sdk.AccAddress, _ math.Int) error {
		if a.String() == f.staker {
			return errors.New("store failure")
		}
		return nil
	}

	h := c.Tranches[0].StakeDeadline
	ctx := f.sdkCtx.WithBlockHeight(h)
	// Failures 1 and 2: +100 blocks each, counted.
	for want := uint32(1); want < maxFailuresForTest; want++ {
		ctx = ctx.WithBlockHeight(h).WithEventManager(sdk.NewEventManager())
		require.NoError(t, f.keeper.ProcessDeadlines(ctx))
		failures, err := f.keeper.DeadlineFailures.Get(ctx, contribID)
		require.NoError(t, err)
		require.Equal(t, want, failures)
		next, err := f.keeper.ContributionNextDeadline.Get(ctx, contribID)
		require.NoError(t, err)
		require.Equal(t, h+100, next)
		require.Equal(t, 0, countEvents(ctx, "contribution_deadline_poisoned"))
		h = next
	}

	// Failure 3: poisoned. Entry parked at the next epoch, count saturated.
	ctx = ctx.WithBlockHeight(h).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.ProcessDeadlines(ctx))
	require.Equal(t, 1, countEvents(ctx, "contribution_deadline_poisoned"))
	failures, err := f.keeper.DeadlineFailures.Get(ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, maxFailuresForTest, failures)
	next, err := f.keeper.ContributionNextDeadline.Get(ctx, contribID)
	require.NoError(t, err, "a poisoned contribution stays in the index")
	nextEpoch, err := f.repKeeper.NextEpochStartHeight(ctx)
	require.NoError(t, err)
	require.Equal(t, nextEpoch, next)
	c, err = f.keeper.Contribution.Get(ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS, c.Status)

	// Still failing an epoch later: no second poisoned event, count stays
	// saturated, next retry one more epoch out.
	ctx = ctx.WithBlockHeight(next).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.ProcessDeadlines(ctx))
	require.Equal(t, 0, countEvents(ctx, "contribution_deadline_poisoned"))
	require.Equal(t, 1, countEvents(ctx, "contribution_deadline_failed"))
	failures, err = f.keeper.DeadlineFailures.Get(ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, maxFailuresForTest, failures)
	next, err = f.keeper.ContributionNextDeadline.Get(ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, ctx.BlockHeight()+mockEpochBlocks, next)

	// The fault clears: the next epoch retry succeeds and the count goes.
	f.repKeeper.unlockDREAMFn = nil
	ctx = ctx.WithBlockHeight(next)
	require.NoError(t, f.keeper.ProcessDeadlines(ctx))
	c, err = f.keeper.Contribution.Get(ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, c.Status)
	_, err = f.keeper.DeadlineFailures.Get(ctx, contribID)
	require.ErrorIs(t, err, collections.ErrNotFound)
	_, err = f.keeper.ContributionNextDeadline.Get(ctx, contribID)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

// TestREV202_SaveClearsDeadlineFailures: a save of the contribution (a tx
// write, or one that takes it out of IN_PROGRESS) resets the count, so
// "three consecutive" never mixes old and new failures and the counter
// does not leak for finished contributions.
func TestREV202_SaveClearsDeadlineFailures(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	require.NoError(t, f.keeper.DeadlineFailures.Set(f.ctx, contribID, 2))

	// A tx write (a stake) while IN_PROGRESS clears it.
	f.stakeOnTranche(t, contribID, 0, f.staker, 300)
	_, err := f.keeper.DeadlineFailures.Get(f.ctx, contribID)
	require.ErrorIs(t, err, collections.ErrNotFound)

	// A write that leaves IN_PROGRESS clears it too and drops the index entry.
	require.NoError(t, f.keeper.DeadlineFailures.Set(f.ctx, contribID, 2))
	c, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	c.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED
	require.NoError(t, f.keeper.SetContribution(f.ctx, c))
	_, err = f.keeper.DeadlineFailures.Get(f.ctx, contribID)
	require.ErrorIs(t, err, collections.ErrNotFound)
	_, err = f.keeper.ContributionNextDeadline.Get(f.ctx, contribID)
	require.ErrorIs(t, err, collections.ErrNotFound)
}
