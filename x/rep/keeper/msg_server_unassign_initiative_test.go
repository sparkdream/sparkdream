package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerUnassignInitiative(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.UnassignInitiative(f.ctx, &types.MsgUnassignInitiative{
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

		_, err = ms.UnassignInitiative(f.ctx, &types.MsgUnassignInitiative{
			Creator:      assigneeStr,
			InitiativeId: 99999,
			Reason:       "Test",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get initiative")
	})

	t.Run("not the assignee", func(t *testing.T) {
		// Everyone but the bystander is authorized, so project setup still
		// works and the only party this can fail on is the bystander: not the
		// assignee, and not on the operations committee.
		otherUser := sdk.AccAddress([]byte("other"))
		f := initFixture(t, WithAuthorizationPolicy(
			func(addr sdk.AccAddress, _ string, _ string) bool {
				return !addr.Equals(otherUser)
			}))
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project, assignee, and initiative
		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
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
		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)
		k.AssignInitiativeToMember(ctx, initID, assignee)

		// A bystander is neither the assignee nor on the operations
		// committee, so the authorization check rejects them before the
		// keeper is reached.
		otherUserStr, err := f.addressCodec.BytesToString(otherUser)
		require.NoError(t, err)

		_, err = ms.UnassignInitiative(ctx, &types.MsgUnassignInitiative{
			Creator:      otherUserStr,
			InitiativeId: initID,
			Reason:       "Test",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "only the assignee or the operations committee")
	})

	t.Run("successful unassign - by assignee", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project, assignee member, and initiative
		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
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
		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)
		k.AssignInitiativeToMember(ctx, initID, assignee)

		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		// Verify initial state
		projectBefore, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, budget.String(), projectBefore.AllocatedBudget.String())

		// Step down from the initiative
		_, err = ms.UnassignInitiative(ctx, &types.MsgUnassignInitiative{
			Creator:      assigneeStr,
			InitiativeId: initID,
			Reason:       "No longer needed",
		})
		require.NoError(t, err)

		// The initiative goes back on the board with nobody holding it.
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_OPEN, initiative.Status)
		require.Empty(t, initiative.Assignee)

		// The budget stays allocated: the work is still live and still funded
		// for whoever picks it up next.
		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, budget.String(), project.AllocatedBudget.String())
	})
}
