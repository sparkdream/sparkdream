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

	// Proposal-time hard cap on requested_budget. The cap is set well above any
	// legitimate project size (default 1M DREAM) — its only job is to stop
	// nonsense values that would otherwise sit in state. We pin the cap to a
	// small value so the test doesn't need to allocate 1M+ math.Ints.
	t.Run("rejects requested_budget over cap", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		ms := keeper.NewMsgServerImpl(k)

		params, err := k.Params.Get(ctx)
		require.NoError(t, err)
		params.MaxProjectRequestedBudget = math.NewInt(1000)
		require.NoError(t, k.Params.Set(ctx, params))

		creator := sdk.AccAddress([]byte("creator-over-bud"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: make(map[string]string),
		})
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.ProposeProject(ctx, &types.MsgProposeProject{
			Creator:         creatorStr,
			Name:            "Too big",
			Description:     "over the cap",
			Tags:            []string{"infra"},
			Category:        types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
			Council:         "technical",
			RequestedBudget: keeper.PtrInt(math.NewInt(1001)), // cap + 1
			RequestedSpark:  keeper.PtrInt(math.ZeroInt()),
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrRequestedBudgetExceedsCap)

		// Boundary: exactly at the cap should pass.
		_, err = ms.ProposeProject(ctx, &types.MsgProposeProject{
			Creator:         creatorStr,
			Name:            "At cap",
			Description:     "exactly at the cap",
			Tags:            []string{"infra"},
			Category:        types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
			Council:         "technical",
			RequestedBudget: keeper.PtrInt(math.NewInt(1000)),
			RequestedSpark:  keeper.PtrInt(math.ZeroInt()),
		})
		require.NoError(t, err)
	})

	t.Run("rejects requested_spark over cap", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		ms := keeper.NewMsgServerImpl(k)

		params, err := k.Params.Get(ctx)
		require.NoError(t, err)
		params.MaxProjectRequestedSpark = math.NewInt(100)
		require.NoError(t, k.Params.Set(ctx, params))

		creator := sdk.AccAddress([]byte("creator-over-spk"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: make(map[string]string),
		})
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.ProposeProject(ctx, &types.MsgProposeProject{
			Creator:         creatorStr,
			Name:            "Too sparky",
			Description:     "over the SPARK cap",
			Tags:            []string{"infra"},
			Category:        types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
			Council:         "technical",
			RequestedBudget: keeper.PtrInt(math.ZeroInt()),
			RequestedSpark:  keeper.PtrInt(math.NewInt(101)), // cap + 1
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrRequestedSparkExceedsCap)
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
