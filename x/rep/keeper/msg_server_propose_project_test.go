package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerProposeProject(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ProposeProject(f.ctx, &types.MsgProposeProject{
			Creator:         "invalid-address",
			Name:            "Project",
			Description:     "Desc",
			Tags:            []string{"tag"},
			Category:        1,
			Council:         "technical",
			RequestedBudget: keeper.PtrInt(math.NewInt(1000)),
			RequestedSpark:  keeper.PtrInt(math.NewInt(100)),
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

		_, err = ms.ProposeProject(f.ctx, &types.MsgProposeProject{
			Creator:         creatorStr,
			Name:            "Project",
			Description:     "Desc",
			Tags:            []string{"tag"},
			Category:        1,
			Council:         "technical",
			RequestedBudget: keeper.PtrInt(math.NewInt(1000)),
			RequestedSpark:  keeper.PtrInt(math.NewInt(100)),
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "creator must be a member")
	})

	t.Run("successful proposal", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create member
		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		// Propose project
		_, err = ms.ProposeProject(ctx, &types.MsgProposeProject{
			Creator:         creatorStr,
			Name:            "New Project",
			Description:     "Project description",
			Tags:            []string{"backend"},
			Category:        types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
			Council:         "technical",
			RequestedBudget: keeper.PtrInt(math.NewInt(1000)),
			RequestedSpark:  keeper.PtrInt(math.NewInt(100)),
			Deliverables:    []string{"feature1"},
			Milestones:      []string{"milestone1"},
		})
		require.NoError(t, err)

		// Verify project exists
		var project types.Project
		found := false
		err = k.Project.Walk(ctx, nil, func(id uint64, p types.Project) (bool, error) {
			project = p
			found = true
			return true, nil
		})
		require.NoError(t, err)
		require.True(t, found, "project should exist")
		require.Equal(t, "New Project", project.Name)
	})
}
