package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// cancelTestApprover is the Operations Committee member that approves the
// project in setupCancellableInitiative. Fixtures that narrow the
// authorization policy must keep this address authorized.
var cancelTestApprover = sdk.AccAddress([]byte("approver"))

// setupCancellableInitiative creates an approved project with one OPEN,
// unassigned initiative and returns the project and initiative IDs.
func setupCancellableInitiative(t *testing.T, k keeper.Keeper, ctx sdk.Context, creator sdk.AccAddress, budget math.Int) (uint64, uint64) {
	t.Helper()

	projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, cancelTestApprover, math.NewInt(10000), math.NewInt(1000)))

	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", budget)
	require.NoError(t, err)

	return projectID, initID
}

func TestMsgServerCancelInitiative(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CancelInitiative(f.ctx, &types.MsgCancelInitiative{
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

		creatorStr, err := f.addressCodec.BytesToString(sdk.AccAddress([]byte("creator")))
		require.NoError(t, err)

		_, err = ms.CancelInitiative(f.ctx, &types.MsgCancelInitiative{
			Creator:      creatorStr,
			InitiativeId: 99999,
			Reason:       "Test",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get initiative")
	})

	t.Run("rejects non-creator, non-ops caller", func(t *testing.T) {
		// Only the approver is on the committee, so the caller below is neither
		// the project creator nor an Operations Committee member.
		f := initFixture(t, WithAuthorizationPolicy(AuthorizeAddresses(cancelTestApprover)))
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator"))
		_, initID := setupCancellableInitiative(t, f.keeper, f.ctx, creator, math.NewInt(100))

		otherStr, err := f.addressCodec.BytesToString(sdk.AccAddress([]byte("other")))
		require.NoError(t, err)

		_, err = ms.CancelInitiative(f.ctx, &types.MsgCancelInitiative{
			Creator:      otherStr,
			InitiativeId: initID,
			Reason:       "Test",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("operations committee may cancel", func(t *testing.T) {
		ops := sdk.AccAddress([]byte("ops-member"))
		f := initFixture(t, WithAuthorizationPolicy(AuthorizeAddresses(cancelTestApprover, ops)))
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator"))
		_, initID := setupCancellableInitiative(t, f.keeper, f.ctx, creator, math.NewInt(100))

		opsStr, err := f.addressCodec.BytesToString(ops)
		require.NoError(t, err)

		_, err = ms.CancelInitiative(f.ctx, &types.MsgCancelInitiative{
			Creator:      opsStr,
			InitiativeId: initID,
			Reason:       "Stale listing",
		})
		require.NoError(t, err)

		initiative, err := f.keeper.GetInitiative(f.ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CANCELLED, initiative.Status)
	})

	t.Run("successful cancel by project creator returns budget", func(t *testing.T) {
		// Creator is not on the committee — authority comes from owning the project.
		f := initFixture(t, WithAuthorizationPolicy(AuthorizeAddresses(cancelTestApprover)))
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator"))
		budget := math.NewInt(100)
		projectID, initID := setupCancellableInitiative(t, k, ctx, creator, budget)

		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		// Budget is allocated while the initiative is open
		projectBefore, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, budget.String(), projectBefore.AllocatedBudget.String())

		_, err = ms.CancelInitiative(ctx, &types.MsgCancelInitiative{
			Creator:      creatorStr,
			InitiativeId: initID,
			Reason:       "No longer needed",
		})
		require.NoError(t, err)

		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CANCELLED, initiative.Status)

		// Budget returned to the project
		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, math.ZeroInt().String(), project.AllocatedBudget.String())
	})

	t.Run("rejects assigned initiative", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator"))
		_, initID := setupCancellableInitiative(t, k, ctx, creator, math.NewInt(100))

		assignee := sdk.AccAddress([]byte("assignee"))
		require.NoError(t, k.Member.Set(ctx, assignee.String(), types.Member{
			Address:          assignee.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		}))
		require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))

		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.CancelInitiative(ctx, &types.MsgCancelInitiative{
			Creator:      creatorStr,
			InitiativeId: initID,
			Reason:       "Reclaiming",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "must be in OPEN status")

		// Assignment survives the rejected cancel
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, initiative.Status)
		require.Equal(t, assignee.String(), initiative.Assignee)
	})

	t.Run("rejects double cancel", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator"))
		projectID, initID := setupCancellableInitiative(t, f.keeper, f.ctx, creator, math.NewInt(100))

		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		msg := &types.MsgCancelInitiative{Creator: creatorStr, InitiativeId: initID, Reason: "Test"}
		_, err = ms.CancelInitiative(f.ctx, msg)
		require.NoError(t, err)

		_, err = ms.CancelInitiative(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "must be in OPEN status")

		// Budget was returned exactly once
		project, err := f.keeper.GetProject(f.ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, math.ZeroInt().String(), project.AllocatedBudget.String())
	})
}
