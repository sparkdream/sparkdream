package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

func TestMsgResolveDispute_Accept(t *testing.T) {
	f := initTestFixture(t)

	// Set up a contribution with a DISPUTED tranche
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)

	// Manually set tranche to DISPUTED
	contrib, _ := f.keeper.Contribution.Get(f.ctx, contribID)
	contrib.Tranches[0].Status = types.TrancheStatus_TRANCHE_STATUS_DISPUTED
	require.NoError(t, f.keeper.Contribution.Set(f.ctx, contribID, contrib))

	_, err := f.msgServer.ResolveDispute(f.ctx, &types.MsgResolveDispute{
		Authority:      f.authority,
		Proposer:       f.authority,
		ContributionId: contribID,
		TrancheId:      0,
		Verdict:        types.DisputeVerdict_DISPUTE_VERDICT_ACCEPT,
		Reason:         "Code is fine",
	})
	require.NoError(t, err)

	// Tranche should be VERIFIED after ACCEPT
	contrib, err = f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_VERIFIED, contrib.Tranches[0].Status)
}

func TestMsgResolveDispute_Improve(t *testing.T) {
	f := initTestFixture(t)

	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)

	// Cast a vote first so we can verify it gets deleted
	f.castVerifyVote(t, contribID, 0, f.staker, false, 2)

	// Set to DISPUTED
	contrib, _ := f.keeper.Contribution.Get(f.ctx, contribID)
	contrib.Tranches[0].Status = types.TrancheStatus_TRANCHE_STATUS_DISPUTED
	require.NoError(t, f.keeper.Contribution.Set(f.ctx, contribID, contrib))

	_, err := f.msgServer.ResolveDispute(f.ctx, &types.MsgResolveDispute{
		Authority:      f.authority,
		Proposer:       f.authority,
		ContributionId: contribID,
		TrancheId:      0,
		Verdict:        types.DisputeVerdict_DISPUTE_VERDICT_IMPROVE,
		Reason:         "Needs improvement",
	})
	require.NoError(t, err)

	// Tranche should be BACKED (re-reveal)
	contrib, err = f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_BACKED, contrib.Tranches[0].Status)
	require.Empty(t, contrib.Tranches[0].CodeUri)
	require.Empty(t, contrib.Tranches[0].CommitHash)
	require.True(t, contrib.Tranches[0].RevealDeadline > 0)

	// Vote should be deleted
	vk := keeper.VoteKey(contribID, 0, f.staker)
	_, err = f.keeper.Vote.Get(f.ctx, vk)
	require.Error(t, err)
}

func TestMsgResolveDispute_Reject(t *testing.T) {
	f := initTestFixture(t)

	contribID := f.createDefaultProposal(t) // 2 tranches
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 5000) // backs first tranche

	// Reveal first tranche
	f.revealTranche(t, contribID, 0)

	// Set to DISPUTED
	contrib, _ := f.keeper.Contribution.Get(f.ctx, contribID)
	contrib.Tranches[0].Status = types.TrancheStatus_TRANCHE_STATUS_DISPUTED
	require.NoError(t, f.keeper.Contribution.Set(f.ctx, contribID, contrib))

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

	_, err := f.msgServer.ResolveDispute(f.ctx, &types.MsgResolveDispute{
		Authority:      f.authority,
		Proposer:       f.authority,
		ContributionId: contribID,
		TrancheId:      0,
		Verdict:        types.DisputeVerdict_DISPUTE_VERDICT_REJECT,
		Reason:         "Plagiarized code",
	})
	require.NoError(t, err)
	require.True(t, burnCalled)
	require.True(t, deductCalled)

	contrib, err = f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, contrib.Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_FAILED, contrib.Tranches[0].Status)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_CANCELLED, contrib.Tranches[1].Status)
}

func TestMsgResolveDispute_UnspecifiedVerdict(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)

	_, err := f.msgServer.ResolveDispute(f.ctx, &types.MsgResolveDispute{
		Authority:      f.authority,
		ContributionId: contribID,
		TrancheId:      0,
		Verdict:        types.DisputeVerdict_DISPUTE_VERDICT_UNSPECIFIED,
	})
	require.ErrorIs(t, err, types.ErrInvalidVerdict)
}

func TestMsgResolveDispute_TrancheNotDisputed(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0) // REVEALED, not DISPUTED

	_, err := f.msgServer.ResolveDispute(f.ctx, &types.MsgResolveDispute{
		Authority:      f.authority,
		ContributionId: contribID,
		TrancheId:      0,
		Verdict:        types.DisputeVerdict_DISPUTE_VERDICT_ACCEPT,
	})
	require.ErrorIs(t, err, types.ErrTrancheNotDisputed)
}
