package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestMsgCancel_ContributorCancelProposed(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)

	unlockCalled := false
	f.repKeeper.unlockDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		unlockCalled = true
		return nil
	}

	_, err := f.msgServer.Cancel(f.ctx, &types.MsgCancel{
		Authority:      f.contributor,
		ContributionId: contribID,
		Reason:         "changed my mind",
	})
	require.NoError(t, err)
	require.True(t, unlockCalled)

	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, contrib.Status)
}

func TestMsgCancel_ContributorCannotCancelAfterBacked(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000) // reaches BACKED

	_, err := f.msgServer.Cancel(f.ctx, &types.MsgCancel{
		Authority:      f.contributor,
		ContributionId: contribID,
		Reason:         "want to cancel",
	})
	require.ErrorIs(t, err, types.ErrCannotCancelBacked)
}

func TestMsgCancel_ContributorCancelDuringStaking(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 10000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000) // partial, still STAKING

	_, err := f.msgServer.Cancel(f.ctx, &types.MsgCancel{
		Authority:      f.contributor,
		ContributionId: contribID,
		Reason:         "abandoning",
	})
	require.NoError(t, err)

	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, contrib.Status)
}

func TestMsgCancel_CommitteeCancelAnytime(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000) // reaches BACKED

	// Committee (authority) cancels — allowed even after BACKED
	_, err := f.msgServer.Cancel(f.ctx, &types.MsgCancel{
		Authority:      f.authority,
		ContributionId: contribID,
		Reason:         "policy violation",
	})
	require.NoError(t, err)

	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, contrib.Status)
}

func TestMsgCancel_NotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.Cancel(f.ctx, &types.MsgCancel{
		Authority:      f.authority,
		ContributionId: 9999,
		Reason:         "gone",
	})
	require.ErrorIs(t, err, types.ErrContributionNotFound)
}
