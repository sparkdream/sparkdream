package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerRemoveStake(t *testing.T) {
	t.Run("invalid staker address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.Unstake(f.ctx, &types.MsgUnstake{
			Staker:  "invalid-address",
			StakeId: 1,
			Amount:  keeper.PtrInt(math.NewInt(50)),
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid staker address")
	})

	t.Run("non-existent stake", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		staker := sdk.AccAddress([]byte("staker"))
		stakerStr, err := f.addressCodec.BytesToString(staker)
		require.NoError(t, err)

		_, err = ms.Unstake(f.ctx, &types.MsgUnstake{
			Staker:  stakerStr,
			StakeId: 99999,
			Amount:  keeper.PtrInt(math.NewInt(50)),
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("successful removal", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create stake
		// Burn sequence to avoid 0 ID
		_, _ = k.StakeSeq.Next(ctx)

		staker := sdk.AccAddress([]byte("staker"))
		k.Member.Set(ctx, staker.String(), types.Member{
			Address:          staker.String(),
			DreamBalance:     PtrInt(math.NewInt(1000)),
			StakedDream:      PtrInt(math.NewInt(1000)),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		// Create project
		projectID, _ := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		initID, err := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))
		require.NoError(t, err)
		stakerStr, err := f.addressCodec.BytesToString(staker)
		require.NoError(t, err)

		stakeID := uint64(100)
		stake := types.Stake{
			Id:            stakeID,
			Staker:        staker.String(),
			TargetType:    types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			TargetId:      initID,
			Amount:        math.NewInt(100),
			CreatedAt:     sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
			LastClaimedAt: 0,
			RewardDebt:    math.ZeroInt(),
		}
		err = k.Stake.Set(ctx, stakeID, stake)
		require.NoError(t, err)

		// Lazy conviction update requires this
		err = k.UpdateInitiativeConvictionLazy(ctx, initID)
		require.NoError(t, err)

		_, err = ms.Unstake(ctx, &types.MsgUnstake{
			Staker:  stakerStr,
			StakeId: stakeID,
			Amount:  keeper.PtrInt(math.NewInt(50)), // Remove 50 of 100
		})
		require.NoError(t, err)

		// Verify stake is reduced (Partial Unstake)
		stake, err = k.GetStake(ctx, stakeID)
		require.NoError(t, err)
		require.Equal(t, math.NewInt(50).String(), stake.Amount.String())

		// Remove remaining 50
		_, err = ms.Unstake(ctx, &types.MsgUnstake{
			Staker:  stakerStr,
			StakeId: stakeID,
			Amount:  keeper.PtrInt(math.NewInt(50)),
		})
		require.NoError(t, err)

		// Verify stake is now removed (Full Unstake)
		_, err = k.GetStake(ctx, stakeID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})
}
