package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestMsgWithdraw_Success(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 10000)
	f.approveContribution(t, contribID)

	stakeID := f.stakeOnTranche(t, contribID, 0, f.staker, 1000)

	unlockCalled := false
	f.repKeeper.unlockDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		unlockCalled = true
		return nil
	}

	_, err := f.msgServer.Withdraw(f.ctx, &types.MsgWithdraw{
		Staker:  f.staker,
		StakeId: stakeID,
	})
	require.NoError(t, err)
	require.True(t, unlockCalled)

	// Stake should be removed
	_, err = f.keeper.RevealStake.Get(f.ctx, stakeID)
	require.Error(t, err)
}

func TestMsgWithdraw_RevertsBackedToStaking(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)

	// Stake enough to reach BACKED
	stakeID := f.stakeOnTranche(t, contribID, 0, f.staker, 1000)

	// Verify BACKED
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_BACKED, contrib.Tranches[0].Status)

	// Withdraw — should revert to STAKING
	_, err = f.msgServer.Withdraw(f.ctx, &types.MsgWithdraw{
		Staker:  f.staker,
		StakeId: stakeID,
	})
	require.NoError(t, err)

	contrib, err = f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_STAKING, contrib.Tranches[0].Status)
	require.Equal(t, int64(0), contrib.Tranches[0].BackedAt)
	require.Equal(t, int64(0), contrib.Tranches[0].RevealDeadline)
}

func TestMsgWithdraw_NotOwner(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 10000)
	f.approveContribution(t, contribID)

	stakeID := f.stakeOnTranche(t, contribID, 0, f.staker, 1000)

	_, err := f.msgServer.Withdraw(f.ctx, &types.MsgWithdraw{
		Staker:  f.staker2,
		StakeId: stakeID,
	})
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func TestMsgWithdraw_BlockedDuringRevealed(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)

	stakeID := f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0) // BACKED → REVEALED

	_, err := f.msgServer.Withdraw(f.ctx, &types.MsgWithdraw{
		Staker:  f.staker,
		StakeId: stakeID,
	})
	require.ErrorIs(t, err, types.ErrWithdrawalNotAllowed)
}

func TestMsgWithdraw_StakeNotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.Withdraw(f.ctx, &types.MsgWithdraw{
		Staker:  f.staker,
		StakeId: 9999,
	})
	require.ErrorIs(t, err, types.ErrStakeNotFound)
}
