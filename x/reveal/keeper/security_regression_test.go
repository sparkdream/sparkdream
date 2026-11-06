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
	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// setupRevealedSingleTranche drives a single-tranche contribution of
// `valuation` to REVEALED with two yes votes, min votes lowered to 2.
func setupRevealedSingleTranche(t *testing.T, f *testFixture, valuation int64) uint64 {
	t.Helper()
	contribID := f.createSingleTrancheProposal(t, valuation)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, valuation/2)
	f.stakeOnTranche(t, contribID, 0, f.staker2, valuation-valuation/2)
	f.revealTranche(t, contribID, 0)

	params := testParams()
	params.MinVerificationVotes = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	f.castVerifyVote(t, contribID, 0, f.staker, true, 5)
	f.castVerifyVote(t, contribID, 0, f.staker2, true, 5)
	return contribID
}

// REV-01: a staker whose lock is gone (zeroed by x/rep) used to make the
// stake return fail after the payout mint; nothing was saved, so every
// following block minted the tranche payout again. Now the zeroed stake is
// dropped and the payout is minted exactly once.
func TestRegression_REV01_ZeroedStakerNoRepeatedPayout(t *testing.T) {
	f := initTestFixture(t)
	contribID := setupRevealedSingleTranche(t, f, 1000)
	f.advanceBlockHeight(15)

	mints := 0
	f.repKeeper.mintDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		mints++
		return nil
	}
	f.repKeeper.unlockDREAMFn = func(_ context.Context, addr sdk.AccAddress, _ math.Int) error {
		if addr.Equals(f.staker2Addr) {
			return reptypes.ErrInsufficientStake // staker2 was zeroed
		}
		return nil
	}

	for i := 0; i < 5; i++ {
		require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))
		f.advanceBlockHeight(1)
	}

	// Immediate payout + holdback release on completion, once each.
	require.Equal(t, 2, mints)
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED, contrib.Status)

	// Every stake record is gone, including the zeroed staker's.
	resp, err := f.queryServer.TrancheStakes(f.ctx, &types.QueryTrancheStakesRequest{ContributionId: contribID, TrancheId: 0})
	require.NoError(t, err)
	require.Empty(t, resp.Stakes)
}

// REV-01: any other unlock failure rolls the whole contribution back; the
// payout is not minted until the step succeeds.
func TestRegression_REV01_FailedStepRollsBack(t *testing.T) {
	f := initTestFixture(t)
	contribID := setupRevealedSingleTranche(t, f, 1000)
	f.advanceBlockHeight(15)

	failing := true
	unlocks := 0
	f.repKeeper.unlockDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		if failing {
			return errors.New("store failure")
		}
		unlocks++
		return nil
	}
	mints := 0
	f.repKeeper.mintDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		mints++
		return nil
	}

	for i := 0; i < 3; i++ {
		require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))
		f.advanceBlockHeight(1)
	}
	require.Zero(t, mints)
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_REVEALED, contrib.Tranches[0].Status)

	// The failed entry was parked for the retry window (REV-104).
	failing = false
	f.advanceBlockHeight(keeper.DeadlineRetryBlocks)
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))
	require.Equal(t, 2, mints)
	require.Equal(t, 3, unlocks) // two stakes + bond, once each
	contrib, err = f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED, contrib.Status)
}

// REV-08: a holdback mint refused by the epoch cap is retried instead of
// being dropped on a COMPLETED record.
func TestRegression_REV08_HoldbackMintRetried(t *testing.T) {
	f := initTestFixture(t)
	contribID := setupRevealedSingleTranche(t, f, 1000)
	f.advanceBlockHeight(15)

	capHit := true
	f.repKeeper.mintDREAMFn = func(_ context.Context, _ sdk.AccAddress, amount math.Int) error {
		if capHit && amount.Equal(math.NewInt(200)) { // 20% holdback of 1000
			return errors.New("epoch mint cap reached")
		}
		return nil
	}

	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS, contrib.Status)

	capHit = false
	f.advanceBlockHeight(keeper.DeadlineRetryBlocks) // parked for the retry window (REV-104)
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))
	contrib, err = f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED, contrib.Status)
	require.True(t, contrib.HoldbackAmount.IsZero())

	has, err := f.keeper.ContributionsByStatus.Has(f.ctx, collections.Join(int32(types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED), contribID))
	require.NoError(t, err)
	require.True(t, has)
}

// REV-02: approve -> back -> (stake deadline passes) -> a staker tries to
// withdraw -> reveal deadline passes. The withdrawal is refused, so the
// contributor cannot turn a missed delivery into a "stake timeout" that
// returns the bond: the reveal timeout slashes it.
func TestRegression_REV02_BackedWithdrawCannotEscapeSlash(t *testing.T) {
	f := initTestFixture(t)
	params := testParams()
	params.RevealDeadlineEpochs = 100 // reveal window outlives the stake deadline
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.advanceBlockHeight(55)
	stakeID := f.stakeOnTranche(t, contribID, 0, f.staker, 1000) // BACKED
	f.advanceBlockHeight(10)                                     // past the stake deadline (60)

	_, err := f.msgServer.Withdraw(f.ctx, &types.MsgWithdraw{Staker: f.staker, StakeId: stakeID})
	require.ErrorIs(t, err, types.ErrWithdrawalNotAllowed)

	burned := math.ZeroInt()
	f.repKeeper.burnDREAMFn = func(_ context.Context, _ sdk.AccAddress, amount math.Int) error {
		burned = burned.Add(amount)
		return nil
	}

	// The stake deadline has passed but nothing is cancelled.
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS, contrib.Status)
	bond := contrib.BondRemaining

	// Reveal deadline passes: the bond is slashed.
	f.advanceBlockHeight(100)
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))
	contrib, err = f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, contrib.Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_FAILED, contrib.Tranches[0].Status)
	require.True(t, bond.IsPositive())
	require.Equal(t, bond, burned)
}

// REV-03: reputation is computed in DREAM, not micro-DREAM, and capped.
func TestRegression_REV03_ReputationScale(t *testing.T) {
	f := initTestFixture(t)
	params := testParams()
	params.MaxTrancheValuation = math.NewInt(100_000_000_000)
	params.MaxTotalValuation = math.NewInt(100_000_000_000)
	params.MinStakeAmount = math.NewInt(1)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// 2,000 DREAM tranche at quality 5 -> 2000/1000 * 5/5 = 2 reputation.
	const valuation = 2_000_000_000
	contribID := f.createSingleTrancheProposal(t, valuation)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, valuation/2)
	f.stakeOnTranche(t, contribID, 0, f.staker2, valuation/2)
	f.revealTranche(t, contribID, 0)
	p, _ := f.keeper.Params.Get(f.ctx)
	p.MinVerificationVotes = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))
	f.castVerifyVote(t, contribID, 0, f.staker, true, 5)
	f.castVerifyVote(t, contribID, 0, f.staker2, true, 5)
	f.advanceBlockHeight(15)

	var granted math.LegacyDec
	f.repKeeper.addReputationFn = func(_ context.Context, _ sdk.AccAddress, _ string, amount math.LegacyDec) error {
		granted = amount
		return nil
	}
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))
	require.Equal(t, math.LegacyNewDec(2), granted)

	// A 50,000 DREAM tranche is capped instead of granting 50,000,000.
	require.Equal(t, keeper.RevealReputationCapPerTranche,
		keeper.TrancheReputationForTest(math.NewInt(50_000_000_000), math.LegacyNewDec(5)))
}

// REV-04: an individual Operations Committee member cannot cancel (and so
// cannot release the holdback); the committee policy address can.
func TestRegression_REV04_SingleMemberCannotCancel(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createDefaultProposal(t)
	f.approveContribution(t, contribID)

	member := sdk.AccAddress([]byte("committee_member____"))
	policy := sdk.AccAddress([]byte("ops_policy__________"))
	f.commonsKeeper.isCommitteeMemberFn = func(_ context.Context, _ sdk.AccAddress, _, _ string) (bool, error) { return true, nil }
	f.commonsKeeper.isCouncilAuthorizedFn = func(_ context.Context, _, _, _ string) bool { return true }
	f.commonsKeeper.isCouncilOrCommitteeFn = func(_ context.Context, addr, council, committee string) bool {
		return addr == policy.String() && council == "commons" && committee == "operations"
	}

	_, err := f.msgServer.Cancel(f.ctx, &types.MsgCancel{Authority: member.String(), ContributionId: contribID, Reason: "collusion"})
	require.ErrorIs(t, err, types.ErrUnauthorized)

	_, err = f.msgServer.Cancel(f.ctx, &types.MsgCancel{Authority: policy.String(), ContributionId: contribID, Reason: "committee decision"})
	require.NoError(t, err)
}

// REV-06 / RVL-2: a zeroed contributor used to make the dispute timeout fail
// before the stakes were returned, stranding every staker's DREAM and
// retrying every block. Now the stakes come back and the contribution ends.
func TestRegression_REV06_DisputeTimeoutZeroedContributor(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)
	contrib, _ := f.keeper.Contribution.Get(f.ctx, contribID)
	contrib.Tranches[0].Status = types.TrancheStatus_TRANCHE_STATUS_DISPUTED
	contrib.Tranches[0].VerificationDeadline = 5
	require.NoError(t, f.keeper.Contribution.Set(f.ctx, contribID, contrib))

	stakerUnlocked := false
	f.repKeeper.unlockDREAMFn = func(_ context.Context, addr sdk.AccAddress, _ math.Int) error {
		if addr.Equals(f.contributorAddr) {
			return reptypes.ErrInsufficientStake
		}
		stakerUnlocked = true
		return nil
	}
	burns := 0
	f.repKeeper.burnDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		burns++
		return nil
	}

	f.advanceBlockHeight(36)
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))
	require.True(t, stakerUnlocked)
	require.Zero(t, burns)
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, contrib.Status)
	require.True(t, contrib.BondRemaining.IsZero())
	resp, err := f.queryServer.TrancheStakes(f.ctx, &types.QueryTrancheStakesRequest{ContributionId: contribID, TrancheId: 0})
	require.NoError(t, err)
	require.Empty(t, resp.Stakes)

	// Not retried on the next block.
	f.advanceBlockHeight(1)
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))
	require.Zero(t, burns)
}

// REV-10: verification votes are deleted once the tranche is verified.
func TestRegression_REV10_VotesPrunedOnVerify(t *testing.T) {
	f := initTestFixture(t)
	contribID := setupRevealedSingleTranche(t, f, 1000)
	f.advanceBlockHeight(15)
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))

	n := 0
	require.NoError(t, f.keeper.Vote.Walk(f.ctx, nil, func(string, types.VerificationVote) (bool, error) {
		n++
		return false, nil
	}))
	require.Zero(t, n)
	contrib, _ := f.keeper.Contribution.Get(f.ctx, contribID)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_VERIFIED, contrib.Tranches[0].Status)
}

// REV-11: the deadline index tracks the earliest pending deadline, drops
// finished contributions and is rebuilt by InitGenesis.
func TestRegression_REV11_DeadlineIndex(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)

	_, err := f.keeper.ContributionNextDeadline.Get(f.ctx, contribID)
	require.Error(t, err) // PROPOSED: not indexed

	f.approveContribution(t, contribID)
	d, err := f.keeper.ContributionNextDeadline.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, f.sdkCtx.BlockHeight()+60, d) // stake deadline

	f.stakeOnTranche(t, contribID, 0, f.staker, 1000) // BACKED
	d, err = f.keeper.ContributionNextDeadline.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, f.sdkCtx.BlockHeight()+14, d) // reveal deadline
	has, err := f.keeper.ContributionDeadlines.Has(f.ctx, collections.Join(d, contribID))
	require.NoError(t, err)
	require.True(t, has)

	// Genesis round trip rebuilds the index.
	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	g := initTestFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gs))
	d2, err := g.keeper.ContributionNextDeadline.Get(g.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, d, d2)

	// Reveal timeout cancels: the entry is removed.
	f.advanceBlockHeight(15)
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))
	_, err = f.keeper.ContributionNextDeadline.Get(f.ctx, contribID)
	require.Error(t, err)
	has, err = f.keeper.ContributionDeadlines.Has(f.ctx, collections.Join(d, contribID))
	require.NoError(t, err)
	require.False(t, has)
}
