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
			Amount:     keeper.PtrInt(math.NewInt(100)),
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
			DreamBalance:     PtrInt(math.NewInt(1000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		stakerStr, err := f.addressCodec.BytesToString(staker)
		require.NoError(t, err)

		// Create project and initiative
		projectID, _ := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
		initID, _ := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))

		// Create stake
		_, err = ms.Stake(ctx, &types.MsgStake{
			Staker:     stakerStr,
			TargetType: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			TargetId:   initID,
			Amount:     keeper.PtrInt(math.NewInt(100)),
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
		require.Equal(t, math.NewInt(100).String(), stake.Amount.String())
	})
}
