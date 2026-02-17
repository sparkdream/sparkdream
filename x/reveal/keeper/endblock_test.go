package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/reveal/types"
)

// ---------------------------------------------------------------------------
// EndBlock / ProcessDeadlines tests
// ---------------------------------------------------------------------------

func TestProcessDeadlines_NoContributions(t *testing.T) {
	f := initTestFixture(t)

	err := f.keeper.ProcessDeadlines(f.ctx)
	require.NoError(t, err)
}

func TestProcessDeadlines_StakeTimeout(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 10000)
	f.approveContribution(t, contribID)

	// Partial stake (not enough to reach BACKED)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)

	// Advance past stake deadline (default 60 epochs)
	f.advanceBlockHeight(61)

	unlockCount := 0
	f.repKeeper.unlockDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		unlockCount++
		return nil
	}

	err := f.keeper.ProcessDeadlines(f.ctx)
	require.NoError(t, err)

	// Contribution should be cancelled
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, contrib.Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_CANCELLED, contrib.Tranches[0].Status)
	require.True(t, unlockCount > 0) // staker's stake + contributor's bond returned
}

func TestProcessDeadlines_RevealTimeout(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000) // reaches BACKED

	// Advance past reveal deadline (default 14 epochs)
	f.advanceBlockHeight(15)

	burnCalled := false
	f.repKeeper.burnDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		burnCalled = true
		return nil
	}

	deductCalled := false
	f.repKeeper.deductReputationFn = func(_ context.Context, _ sdk.AccAddress, tag string, amount math.LegacyDec) error {
		deductCalled = true
		require.Equal(t, "reveal", tag)
		require.Equal(t, math.LegacyNewDec(20), amount)
		return nil
	}

	err := f.keeper.ProcessDeadlines(f.ctx)
	require.NoError(t, err)
	require.True(t, burnCalled)
	require.True(t, deductCalled)

	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, contrib.Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_FAILED, contrib.Tranches[0].Status)
}

func TestProcessDeadlines_VerificationPass(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)

	// Two stakers to meet min verification votes (3)
	f.stakeOnTranche(t, contribID, 0, f.staker, 500)
	f.stakeOnTranche(t, contribID, 0, f.staker2, 500)

	f.revealTranche(t, contribID, 0)

	// We need 3 votes to meet min verification votes. We have 2 stakers.
	// Let's reduce min verification votes via params.
	params := types.DefaultParams()
	params.MinVerificationVotes = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Cast yes votes from both stakers
	f.castVerifyVote(t, contribID, 0, f.staker, true, 4)
	f.castVerifyVote(t, contribID, 0, f.staker2, true, 5)

	// Advance past verification deadline (14 epochs)
	f.advanceBlockHeight(15)

	mintCalled := false
	f.repKeeper.mintDREAMFn = func(_ context.Context, _ sdk.AccAddress, amount math.Int) error {
		mintCalled = true
		return nil
	}

	err := f.keeper.ProcessDeadlines(f.ctx)
	require.NoError(t, err)
	require.True(t, mintCalled)

	// Single-tranche contribution should be COMPLETED
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED, contrib.Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_VERIFIED, contrib.Tranches[0].Status)
}

func TestProcessDeadlines_VerificationFailDisputed(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)

	f.stakeOnTranche(t, contribID, 0, f.staker, 500)
	f.stakeOnTranche(t, contribID, 0, f.staker2, 500)
	f.revealTranche(t, contribID, 0)

	params := types.DefaultParams()
	params.MinVerificationVotes = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Cast majority no votes
	f.castVerifyVote(t, contribID, 0, f.staker, false, 2)
	f.castVerifyVote(t, contribID, 0, f.staker2, false, 1)

	f.advanceBlockHeight(15)

	err := f.keeper.ProcessDeadlines(f.ctx)
	require.NoError(t, err)

	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_DISPUTED, contrib.Tranches[0].Status)
}

func TestProcessDeadlines_VerificationNoVotesDisputed(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)

	// Set min verification votes to 1 so we can test the extension + no-vote path
	params := types.DefaultParams()
	params.MinVerificationVotes = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Advance past verification deadline — no votes cast
	f.advanceBlockHeight(15)

	// First call: should extend deadline
	err := f.keeper.ProcessDeadlines(f.ctx)
	require.NoError(t, err)

	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	// Should still be REVEALED (extended)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_REVEALED, contrib.Tranches[0].Status)

	// Advance past the extended deadline
	f.advanceBlockHeight(15)

	// Second call: should mark as DISPUTED (no votes after extension)
	err = f.keeper.ProcessDeadlines(f.ctx)
	require.NoError(t, err)

	contrib, err = f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_DISPUTED, contrib.Tranches[0].Status)
}

func TestProcessDeadlines_DisputeTimeout(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)

	// Manually set to DISPUTED with a verification deadline in the past
	contrib, _ := f.keeper.Contribution.Get(f.ctx, contribID)
	contrib.Tranches[0].Status = types.TrancheStatus_TRANCHE_STATUS_DISPUTED
	contrib.Tranches[0].VerificationDeadline = 5 // in the past
	require.NoError(t, f.keeper.Contribution.Set(f.ctx, contribID, contrib))

	// Advance past dispute resolution deadline (default 30 epochs from verification deadline)
	f.advanceBlockHeight(36)

	burnCalled := false
	f.repKeeper.burnDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		burnCalled = true
		return nil
	}

	deductCalled := false
	f.repKeeper.deductReputationFn = func(_ context.Context, _ sdk.AccAddress, tag string, amount math.LegacyDec) error {
		deductCalled = true
		require.Equal(t, "reveal", tag)
		require.Equal(t, math.LegacyNewDec(10), amount)
		return nil
	}

	err := f.keeper.ProcessDeadlines(f.ctx)
	require.NoError(t, err)
	require.True(t, burnCalled)
	require.True(t, deductCalled)

	contrib, err = f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, contrib.Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_FAILED, contrib.Tranches[0].Status)
}

func TestProcessDeadlines_MultiTrancheAdvance(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createDefaultProposal(t) // 2 tranches of 5000 each
	f.approveContribution(t, contribID)

	// Complete first tranche
	f.stakeOnTranche(t, contribID, 0, f.staker, 3000)
	f.stakeOnTranche(t, contribID, 0, f.staker2, 2000)
	f.revealTranche(t, contribID, 0)

	params := types.DefaultParams()
	params.MinVerificationVotes = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	f.castVerifyVote(t, contribID, 0, f.staker, true, 5)
	f.castVerifyVote(t, contribID, 0, f.staker2, true, 4)

	f.advanceBlockHeight(15)

	err := f.keeper.ProcessDeadlines(f.ctx)
	require.NoError(t, err)

	// First tranche should be VERIFIED, second should now be STAKING
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS, contrib.Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_VERIFIED, contrib.Tranches[0].Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_STAKING, contrib.Tranches[1].Status)
	require.Equal(t, uint32(1), contrib.CurrentTranche)
}

// ---------------------------------------------------------------------------
// Full lifecycle tests
// ---------------------------------------------------------------------------

func TestFullLifecycle_SingleTranche(t *testing.T) {
	f := initTestFixture(t)

	params := types.DefaultParams()
	params.MinVerificationVotes = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Track DREAM operations
	mintedAmounts := make([]math.Int, 0)
	f.repKeeper.mintDREAMFn = func(_ context.Context, _ sdk.AccAddress, amount math.Int) error {
		mintedAmounts = append(mintedAmounts, amount)
		return nil
	}

	// 1. Propose
	contribID := f.createSingleTrancheProposal(t, 1000)

	// 2. Approve
	f.approveContribution(t, contribID)

	// 3. Stake to threshold
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)

	// 4. Reveal
	f.revealTranche(t, contribID, 0)

	// 5. Verify
	f.castVerifyVote(t, contribID, 0, f.staker, true, 5)

	// 6. Process deadline
	f.advanceBlockHeight(15)
	err := f.keeper.ProcessDeadlines(f.ctx)
	require.NoError(t, err)

	// 7. Verify completion
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED, contrib.Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_VERIFIED, contrib.Tranches[0].Status)
	require.True(t, contrib.TransitionedToProject)
	require.NotZero(t, contrib.ProjectId)
	require.True(t, len(mintedAmounts) > 0, "should have minted DREAM")
}

func TestFullLifecycle_MultiTranche(t *testing.T) {
	f := initTestFixture(t)

	params := types.DefaultParams()
	params.MinVerificationVotes = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// 1. Propose with 2 tranches
	contribID := f.createDefaultProposal(t)

	// 2. Approve
	f.approveContribution(t, contribID)

	// --- TRANCHE 0 ---
	f.stakeOnTranche(t, contribID, 0, f.staker, 5000)
	f.revealTranche(t, contribID, 0)
	f.castVerifyVote(t, contribID, 0, f.staker, true, 5)

	f.advanceBlockHeight(15)
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))

	// Tranche 0 verified, tranche 1 now staking
	contrib, _ := f.keeper.Contribution.Get(f.ctx, contribID)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_VERIFIED, contrib.Tranches[0].Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_STAKING, contrib.Tranches[1].Status)

	// --- TRANCHE 1 ---
	f.stakeOnTranche(t, contribID, 1, f.staker, 5000)
	f.revealTranche(t, contribID, 1)
	f.castVerifyVote(t, contribID, 1, f.staker, true, 4)

	f.advanceBlockHeight(15)
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))

	// Full completion
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED, contrib.Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_VERIFIED, contrib.Tranches[0].Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_VERIFIED, contrib.Tranches[1].Status)
	require.True(t, contrib.TransitionedToProject)
}

// ---------------------------------------------------------------------------
// EndBlock internal logic tests
// ---------------------------------------------------------------------------

func TestConfirmTranche_HoldbackAccumulation(t *testing.T) {
	f := initTestFixture(t)

	params := types.DefaultParams()
	params.MinVerificationVotes = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	contribID := f.createDefaultProposal(t)
	f.approveContribution(t, contribID)

	// Complete first tranche
	f.stakeOnTranche(t, contribID, 0, f.staker, 5000)
	f.revealTranche(t, contribID, 0)
	f.castVerifyVote(t, contribID, 0, f.staker, true, 4)

	f.advanceBlockHeight(15)
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))

	// Verify holdback was accumulated (20% of 5000 = 1000)
	contrib, _ := f.keeper.Contribution.Get(f.ctx, contribID)
	require.Equal(t, math.NewInt(1000), contrib.HoldbackAmount)
}

func TestCompleteContribution_HoldbackRelease(t *testing.T) {
	f := initTestFixture(t)

	params := types.DefaultParams()
	params.MinVerificationVotes = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	mintedAmounts := make([]math.Int, 0)
	f.repKeeper.mintDREAMFn = func(_ context.Context, _ sdk.AccAddress, amount math.Int) error {
		mintedAmounts = append(mintedAmounts, amount)
		return nil
	}

	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)
	f.castVerifyVote(t, contribID, 0, f.staker, true, 3)

	f.advanceBlockHeight(15)
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))

	contrib, _ := f.keeper.Contribution.Get(f.ctx, contribID)
	require.Equal(t, math.ZeroInt(), contrib.HoldbackAmount, "holdback should be released on completion")
	require.Equal(t, math.ZeroInt(), contrib.BondRemaining, "bond should be returned on completion")

	// Should have minted: immediate payout + holdback release
	require.True(t, len(mintedAmounts) >= 2, "should have minted immediate payout and holdback")
}

func TestCompleteContribution_ProjectCreationFails(t *testing.T) {
	f := initTestFixture(t)

	params := types.DefaultParams()
	params.MinVerificationVotes = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	f.repKeeper.createProjectFn = func(_ context.Context, _ sdk.AccAddress, _, _ string, _ []string, _ reptypes.ProjectCategory, _ string, _, _ math.Int) (uint64, error) {
		return 0, types.ErrContributionNotFound // simulate failure
	}

	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)
	f.castVerifyVote(t, contribID, 0, f.staker, true, 3)

	f.advanceBlockHeight(15)
	// Should NOT fail — project creation failure is logged, not fatal
	require.NoError(t, f.keeper.ProcessDeadlines(f.ctx))

	contrib, _ := f.keeper.Contribution.Get(f.ctx, contribID)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED, contrib.Status)
	require.False(t, contrib.TransitionedToProject, "should not be marked as transitioned since project creation failed")
}
