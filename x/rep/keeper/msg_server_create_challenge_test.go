package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerCreateChallenge(t *testing.T) {
	t.Run("invalid challenger address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CreateChallenge(f.ctx, &types.MsgCreateChallenge{
			Challenger:   "invalid-address",
			InitiativeId: 1,
			Reason:       "Test",
			StakedDream:  keeper.PtrInt(math.NewInt(100)),
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid challenger address")
	})

	t.Run("missing staked DREAM", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		challenger := sdk.AccAddress([]byte("challenger"))
		challengerStr, err := f.addressCodec.BytesToString(challenger)
		require.NoError(t, err)

		_, err = ms.CreateChallenge(f.ctx, &types.MsgCreateChallenge{
			Challenger:   challengerStr,
			InitiativeId: 1,
			Reason:       "Test",
			StakedDream:  nil,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "staked DREAM is required")
	})

	t.Run("non-existent initiative", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		challenger := sdk.AccAddress([]byte("challenger"))
		challengerStr, err := f.addressCodec.BytesToString(challenger)
		require.NoError(t, err)

		_, err = ms.CreateChallenge(f.ctx, &types.MsgCreateChallenge{
			Challenger:   challengerStr,
			InitiativeId: 99999,
			Reason:       "Test",
			StakedDream:  keeper.PtrInt(math.NewInt(100)),
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "collections: not found")
	})

	t.Run("successful creation", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project and initiative
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
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))

		challenger := sdk.AccAddress([]byte("challenger"))
		challengerStr, err := f.addressCodec.BytesToString(challenger)
		require.NoError(t, err)

		// Assign and submit work to make initiative eligible for challenge
		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		k.Member.Set(ctx, assigneeStr, types.Member{
			Address:          assigneeStr,
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "uri")

		// Create challenge
		challenger = sdk.AccAddress([]byte("challenger"))
		challengerStr, err = f.addressCodec.BytesToString(challenger)
		require.NoError(t, err)

		k.Member.Set(ctx, challengerStr, types.Member{
			Address:          challengerStr,
			DreamBalance:     keeper.PtrInt(math.NewInt(1000)),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		_, err = ms.CreateChallenge(ctx, &types.MsgCreateChallenge{
			Challenger:   challengerStr,
			InitiativeId: initID,
			Reason:       "Poor quality",
			Evidence:     []string{"http://example.com/evidence"},
			StakedDream:  keeper.PtrInt(math.NewInt(100)),
		})
		require.NoError(t, err)

		// Verify challenge exists
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, initiative.Status)
	})
}
