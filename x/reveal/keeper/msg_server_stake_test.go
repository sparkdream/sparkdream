package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestMsgStake_Success(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 10000)
	f.approveContribution(t, contribID)

	lockCalled := false
	f.repKeeper.lockDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		lockCalled = true
		return nil
	}

	resp, err := f.msgServer.Stake(f.ctx, &types.MsgStake{
		Staker:         f.staker,
		ContributionId: contribID,
		TrancheId:      0,
		Amount:         math.NewInt(1000),
	})
	require.NoError(t, err)
	require.True(t, lockCalled)

	// Verify stake stored
	stake, err := f.keeper.RevealStake.Get(f.ctx, resp.StakeId)
	require.NoError(t, err)
	require.Equal(t, f.staker, stake.Staker)
	require.Equal(t, contribID, stake.ContributionId)
	require.Equal(t, math.NewInt(1000), stake.Amount)

	// Verify tranche dream_staked updated
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1000), contrib.Tranches[0].DreamStaked)
}

func TestMsgStake_TransitionToBackedWhenThresholdMet(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)

	// Stake the full threshold
	_, err := f.msgServer.Stake(f.ctx, &types.MsgStake{
		Staker:         f.staker,
		ContributionId: contribID,
		TrancheId:      0,
		Amount:         math.NewInt(1000),
	})
	require.NoError(t, err)

	// Tranche should now be BACKED
	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_BACKED, contrib.Tranches[0].Status)
	require.True(t, contrib.Tranches[0].RevealDeadline > 0)
}

func TestMsgStake_NotMember(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)

	f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
		// Contributor is member, staker is not
		return addr.Equals(f.contributorAddr)
	}

	_, err := f.msgServer.Stake(f.ctx, &types.MsgStake{
		Staker:         f.staker,
		ContributionId: contribID,
		TrancheId:      0,
		Amount:         math.NewInt(1000),
	})
	require.ErrorIs(t, err, types.ErrNotMember)
}

func TestMsgStake_SelfStake(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)

	_, err := f.msgServer.Stake(f.ctx, &types.MsgStake{
		Staker:         f.contributor, // contributor trying to stake on own
		ContributionId: contribID,
		TrancheId:      0,
		Amount:         math.NewInt(1000),
	})
	require.ErrorIs(t, err, types.ErrSelfStake)
}

func TestMsgStake_NotInProgress(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	// Don't approve — still PROPOSED

	_, err := f.msgServer.Stake(f.ctx, &types.MsgStake{
		Staker:         f.staker,
		ContributionId: contribID,
		TrancheId:      0,
		Amount:         math.NewInt(1000),
	})
	require.ErrorIs(t, err, types.ErrNotInProgress)
}

func TestMsgStake_TrancheNotStaking(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createDefaultProposal(t) // 2 tranches
	f.approveContribution(t, contribID)

	// Tranche 1 is LOCKED, not STAKING
	_, err := f.msgServer.Stake(f.ctx, &types.MsgStake{
		Staker:         f.staker,
		ContributionId: contribID,
		TrancheId:      1,
		Amount:         math.NewInt(1000),
	})
	require.ErrorIs(t, err, types.ErrTrancheNotStaking)
}

func TestMsgStake_AmountTooLow(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 10000)
	f.approveContribution(t, contribID)

	_, err := f.msgServer.Stake(f.ctx, &types.MsgStake{
		Staker:         f.staker,
		ContributionId: contribID,
		TrancheId:      0,
		Amount:         math.NewInt(50), // min is 100
	})
	require.ErrorIs(t, err, types.ErrStakeAmountTooLow)
}

func TestMsgStake_ExceedsThreshold(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)

	_, err := f.msgServer.Stake(f.ctx, &types.MsgStake{
		Staker:         f.staker,
		ContributionId: contribID,
		TrancheId:      0,
		Amount:         math.NewInt(1001), // threshold is 1000
	})
	require.ErrorIs(t, err, types.ErrStakeExceedsThreshold)
}
