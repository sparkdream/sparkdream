package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerCancelProject(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CancelProject(f.ctx, &types.MsgCancelProject{
			Creator:   "invalid-address",
			ProjectId: 1,
			Reason:    "Test",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("non-existent project", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator"))
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.CancelProject(f.ctx, &types.MsgCancelProject{
			Creator:   creatorStr,
			ProjectId: 99999,
			Reason:    "Test",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to cancel project")
	})

	t.Run("successful cancellation", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project
		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))

		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		// Cancel project
		_, err = ms.CancelProject(ctx, &types.MsgCancelProject{
			Creator:   creatorStr,
			ProjectId: projectID,
			Reason:    "No longer needed",
		})
		require.NoError(t, err)

		// Verify project status
		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_CANCELLED, project.Status)
	})
}
