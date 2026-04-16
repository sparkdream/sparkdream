package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerCreateInitiative(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CreateInitiative(f.ctx, &types.MsgCreateInitiative{
			Creator:   "invalid-address",
			ProjectId: 1,
			Title:     "Task",
			Tier:      1,
			Category:  1,
			Budget:    keeper.PtrInt(math.NewInt(100)),
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("creator not a member", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator"))
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.CreateInitiative(f.ctx, &types.MsgCreateInitiative{
			Creator:   creatorStr,
			ProjectId: 1,
			Title:     "Task",
			Tags:      []string{"tag"},
			Tier:      1,
			Category:  1,
			Budget:    keeper.PtrInt(math.NewInt(100)),
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "creator must be a member")
	})

	t.Run("successful creation", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project and member
		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		// Create initiative
		_, err = ms.CreateInitiative(ctx, &types.MsgCreateInitiative{
			Creator:   creatorStr,
			ProjectId: projectID,
			Title:     "New Task",
			Tags:      []string{"backend"},
			Tier:      1,
			Category:  1,
			Budget:    keeper.PtrInt(math.NewInt(100)),
		})
		require.NoError(t, err)

		// Verify initiative exists
		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, math.NewInt(100).String(), project.AllocatedBudget.String())
	})
}
