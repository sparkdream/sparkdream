package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestMsgReject_Success(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createDefaultProposal(t)

	unlockCalled := false
	f.repKeeper.unlockDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		unlockCalled = true
		return nil
	}

	_, err := f.msgServer.Reject(f.ctx, &types.MsgReject{
		Authority:      f.authority,
		Proposer:       f.authority,
		ContributionId: contribID,
		Reason:         "not aligned with roadmap",
	})
	require.NoError(t, err)
	require.True(t, unlockCalled)

	// Verify status
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, contrib.Status)
	require.True(t, contrib.ProposalEligibleAt > 0)

	// All tranches should be cancelled
	for _, tr := range contrib.Tranches {
		require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_CANCELLED, tr.Status)
	}
}

func TestMsgReject_NotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.Reject(f.ctx, &types.MsgReject{
		Authority:      f.authority,
		Proposer:       f.authority,
		ContributionId: 9999,
		Reason:         "nope",
	})
	require.ErrorIs(t, err, types.ErrContributionNotFound)
}

func TestMsgReject_NotProposed(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createDefaultProposal(t)
	f.approveContribution(t, contribID)

	_, err := f.msgServer.Reject(f.ctx, &types.MsgReject{
		Authority:      f.authority,
		Proposer:       f.authority,
		ContributionId: contribID,
		Reason:         "test",
	})
	require.ErrorIs(t, err, types.ErrContributionNotProposed)
}
