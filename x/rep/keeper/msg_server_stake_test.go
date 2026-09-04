package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerCreateStake(t *testing.T) {
	t.Run("invalid staker address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.Stake(f.ctx, &types.MsgStake{
			Staker:     "invalid-address",
			TargetType: 0,
			TargetId:   1,
			Amount:     keeper.PtrInt(math.NewInt(1000)),
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid staker address")
	})

	t.Run("missing amount", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		staker := sdk.AccAddress([]byte("staker"))
		stakerStr, err := f.addressCodec.BytesToString(staker)
		require.NoError(t, err)

		_, err = ms.Stake(f.ctx, &types.MsgStake{
			Staker:     stakerStr,
			TargetType: 0,
			TargetId:   1,
			Amount:     nil,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "amount is required")
	})

	t.Run("successful creation", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create member with DREAM
		staker := sdk.AccAddress([]byte("staker"))
		k.Member.Set(ctx, staker.String(), types.Member{
			Address:          staker.String(),
			DreamBalance:     PtrInt(math.NewInt(10000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		stakerStr, err := f.addressCodec.BytesToString(staker)
		require.NoError(t, err)

		// Create project and initiative
		projectID, _ := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
		initID, _ := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))

		// Create stake
		_, err = ms.Stake(ctx, &types.MsgStake{
			Staker:     stakerStr,
			TargetType: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			TargetId:   initID,
			Amount:     keeper.PtrInt(math.NewInt(2000)),
		})
		require.NoError(t, err)

		// Verify stake exists
		var stake types.Stake
		found := false
		k.Stake.Walk(ctx, nil, func(id uint64, s types.Stake) (bool, error) {
			stake = s
			found = true
			return true, nil
		})
		require.True(t, found)
		require.Equal(t, math.NewInt(2000).String(), stake.Amount.String())
	})
}

// TestMsgServerStake_TrancheCap asserts the per-target tranche cap surfaces
// through the msg server, not just the keeper. The cap is what bounds the
// conviction queue's work; without it a member could hold an unbounded number
// of stake records on one target for a fully refundable cost.
func TestMsgServerStake_TrancheCap(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	k := f.keeper
	ctx := f.ctx

	staker := sdk.AccAddress([]byte("msg_tranche_staker__"))
	require.NoError(t, k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     keeper.PtrInt(math.NewInt(5_000_000_000)),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "50.0"},
	}))
	stakerStr, err := f.addressCodec.BytesToString(staker)
	require.NoError(t, err)

	projectID, err := k.CreateProject(ctx, staker, "P", "D", []string{"backend"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(100000), math.NewInt(1000)))
	initID, err := k.CreateInitiative(ctx, staker, projectID, "T", "D", []string{"backend"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)

	stakeMsg := &types.MsgStake{
		Staker:     stakerStr,
		TargetType: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		TargetId:   initID,
		Amount:     keeper.PtrInt(math.NewInt(2000)),
	}

	for i := 0; i < types.MaxStakeTranchesPerTarget; i++ {
		_, err := ms.Stake(ctx, stakeMsg)
		require.NoError(t, err, "tranche %d should be accepted", i)
	}

	_, err = ms.Stake(ctx, stakeMsg)
	require.ErrorIs(t, err, types.ErrTooManyStakeTranches)
}

// TestMsgServerClaimAndCompound_Rejections covers the two new msg-server-visible
// rejections: rewards are not collectable before min_stake_duration_seconds, and
// initiative/project stakes cannot compound.
func TestMsgServerClaimAndCompound_Rejections(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	k := f.keeper
	ctx := f.ctx

	staker := sdk.AccAddress([]byte("msg_reject_staker___"))
	require.NoError(t, k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     keeper.PtrInt(math.NewInt(5_000_000_000)),
		StakedDream:      keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "50.0"},
	}))
	stakerStr, err := f.addressCodec.BytesToString(staker)
	require.NoError(t, err)

	projectID, err := k.CreateProject(ctx, staker, "P", "D", []string{"backend"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(100000), math.NewInt(1000)))

	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	t.Run("claim before minimum duration is rejected", func(t *testing.T) {
		_, err := ms.ClaimStakingRewards(ctx, &types.MsgClaimStakingRewards{
			Staker:  stakerStr,
			StakeId: stakeID,
		})
		require.ErrorIs(t, err, types.ErrMinStakeDuration)
	})

	t.Run("compound on a project stake is rejected", func(t *testing.T) {
		_, err := ms.CompoundStakingRewards(ctx, &types.MsgCompoundStakingRewards{
			Staker:  stakerStr,
			StakeId: stakeID,
		})
		require.ErrorIs(t, err, types.ErrCompoundNotSupported)
	})
}
