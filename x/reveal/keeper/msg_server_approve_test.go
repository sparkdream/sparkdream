package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestMsgApprove_Success(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createDefaultProposal(t)

	_, err := f.msgServer.Approve(f.ctx, &types.MsgApprove{
		Authority:      f.authority,
		Proposer:       f.authority,
		ContributionId: contribID,
	})
	require.NoError(t, err)

	// Verify status change
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS, contrib.Status)
	require.Equal(t, f.authority, contrib.ApprovedBy)

	// Tranche 0 should be STAKING with deadline
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_STAKING, contrib.Tranches[0].Status)
	require.True(t, contrib.Tranches[0].StakeDeadline > 0)

	// Tranche 1 should still be LOCKED
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_LOCKED, contrib.Tranches[1].Status)
}

func TestMsgApprove_NotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.Approve(f.ctx, &types.MsgApprove{
		Authority:      f.authority,
		Proposer:       f.authority,
		ContributionId: 9999,
	})
	require.ErrorIs(t, err, types.ErrContributionNotFound)
}

func TestMsgApprove_NotProposed(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createDefaultProposal(t)
	f.approveContribution(t, contribID) // now IN_PROGRESS

	_, err := f.msgServer.Approve(f.ctx, &types.MsgApprove{
		Authority:      f.authority,
		Proposer:       f.authority,
		ContributionId: contribID,
	})
	require.ErrorIs(t, err, types.ErrContributionNotProposed)
}
