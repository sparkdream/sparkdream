package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerAbandonInitiative(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.AbandonInitiative(f.ctx, &types.MsgAbandonInitiative{
			Creator:      "invalid-address",
			InitiativeId: 1,
			Reason:       "Test",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("non-existent initiative", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		_, err = ms.AbandonInitiative(f.ctx, &types.MsgAbandonInitiative{
			Creator:      assigneeStr,
			InitiativeId: 99999,
			Reason:       "Test",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to abandon initiative")
	})

	t.Run("not the assignee", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project, assignee, and initiative
		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		assignee := sdk.AccAddress([]byte("assignee"))
		k.Member.Set(ctx, assignee.String(), types.Member{
			Address:          assignee.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		budget := math.NewInt(100)
		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", budget)
		k.AssignInitiativeToMember(ctx, initID, assignee)

		// Try to abandon with a different user (non-assignee)
		otherUser := sdk.AccAddress([]byte("other"))
		otherUserStr, err := f.addressCodec.BytesToString(otherUser)
		require.NoError(t, err)

		_, err = ms.AbandonInitiative(ctx, &types.MsgAbandonInitiative{
			Creator:      otherUserStr,
			InitiativeId: initID,
			Reason:       "Test",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to abandon initiative")
	})

	t.Run("successful abandon - by assignee", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project, assignee member, and initiative
		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		assignee := sdk.AccAddress([]byte("assignee"))
		k.Member.Set(ctx, assignee.String(), types.Member{
			Address:          assignee.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		budget := math.NewInt(100)
		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", budget)
		k.AssignInitiativeToMember(ctx, initID, assignee)

		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		// Verify initial state
		projectBefore, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, budget.String(), projectBefore.AllocatedBudget.String())

		// Abandon the initiative
		_, err = ms.AbandonInitiative(ctx, &types.MsgAbandonInitiative{
			Creator:      assigneeStr,
			InitiativeId: initID,
			Reason:       "No longer needed",
		})
		require.NoError(t, err)

		// Verify initiative was abandoned
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED, initiative.Status)

		// Verify budget was returned to project
		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, math.ZeroInt().String(), project.AllocatedBudget.String())
	})
}
