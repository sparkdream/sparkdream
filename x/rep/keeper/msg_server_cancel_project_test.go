package keeper_test

import (
	"context"
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
		require.Contains(t, err.Error(), "failed to get project")
	})

	t.Run("successful cancellation by creator", func(t *testing.T) {
		// NeverAuthorized proves authority comes from owning the project, not
		// from committee membership.
		f := initFixture(t, WithAuthorizationPolicy(NeverAuthorized))
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project
		creator := sdk.AccAddress([]byte("creator"))
		require.NoError(t, k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		}))

		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)

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

	t.Run("rejects non-creator, non-ops caller", func(t *testing.T) {
		f := initFixture(t, WithAuthorizationPolicy(NeverAuthorized))
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)

		otherStr, err := f.addressCodec.BytesToString(sdk.AccAddress([]byte("other")))
		require.NoError(t, err)

		_, err = ms.CancelProject(ctx, &types.MsgCancelProject{
			Creator:   otherStr,
			ProjectId: projectID,
			Reason:    "Test",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUnauthorized)

		// Project is untouched by the rejected cancel.
		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_PROPOSED, project.Status)
	})

	// A single Operations Committee member is not the committee: cancelling
	// retires initiatives and settles stakes, so only the council's or its
	// Operations Committee's policy (an executed proposal) or gov may cancel
	// on the committee side.
	t.Run("single operations committee member is refused", func(t *testing.T) {
		f, _, memberStr := cancelGate(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator"))
		projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		require.NoError(t, err)

		ok, err := f.commonsKeeper.IsCommitteeMember(ctx, sdk.AccAddress([]byte("ops-member")), "technical", "operations")
		require.NoError(t, err)
		require.True(t, ok, "fixture must treat the caller as a committee member")

		_, err = ms.CancelProject(ctx, &types.MsgCancelProject{Creator: memberStr, ProjectId: projectID, Reason: "Stale project"})
		require.ErrorIs(t, err, types.ErrUnauthorized)

		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_PROPOSED, project.Status)
	})

	t.Run("committee policy cancels an active budget-backed project", func(t *testing.T) {
		f, policy, _ := cancelGate(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		projectID := newActiveProject(t, k, ctx, sdk.AccAddress([]byte("creator")))

		_, err := ms.CancelProject(ctx, &types.MsgCancelProject{Creator: policy, ProjectId: projectID, Reason: "Committee retires it"})
		require.NoError(t, err)

		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_CANCELLED, project.Status)
	})

	t.Run("policy of another council is refused", func(t *testing.T) {
		f, policy, _ := cancelGate(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		projectID, err := k.CreateProject(ctx, sdk.AccAddress([]byte("creator")), "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "ecosystem", math.NewInt(10000), math.NewInt(1000), false)
		require.NoError(t, err)

		_, err = ms.CancelProject(ctx, &types.MsgCancelProject{Creator: policy, ProjectId: projectID, Reason: "x"})
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("gov authority cancels", func(t *testing.T) {
		f, _, _ := cancelGate(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		projectID := newActiveProject(t, k, ctx, sdk.AccAddress([]byte("creator")))
		gov, err := f.addressCodec.BytesToString(k.GetAuthority())
		require.NoError(t, err)

		_, err = ms.CancelProject(ctx, &types.MsgCancelProject{Creator: gov, ProjectId: projectID, Reason: "gov"})
		require.NoError(t, err)
		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_CANCELLED, project.Status)
	})

	t.Run("creator cannot cancel an approved budget-backed project", func(t *testing.T) {
		f, _, _ := cancelGate(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator"))
		projectID := newActiveProject(t, k, ctx, creator)
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.CancelProject(ctx, &types.MsgCancelProject{Creator: creatorStr, ProjectId: projectID, Reason: "walk away"})
		require.ErrorIs(t, err, types.ErrUnauthorized)

		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_ACTIVE, project.Status)
	})

	t.Run("creator cancels an active permissionless project only once its work is closed", func(t *testing.T) {
		f, _, _ := cancelGate(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator"))
		projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.ZeroInt(), math.ZeroInt(), true)
		require.NoError(t, err)
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		// Another member's open initiative blocks the creator: cancelling would
		// close it (and void any challenge on it).
		require.NoError(t, k.Initiative.Set(ctx, 77, types.Initiative{
			Id:        77,
			ProjectId: projectID,
			Status:    types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
		}))
		_, err = ms.CancelProject(ctx, &types.MsgCancelProject{Creator: creatorStr, ProjectId: projectID, Reason: "early"})
		require.ErrorIs(t, err, types.ErrProjectHasOpenWork)

		// Once the work is terminal the creator may retire their project.
		require.NoError(t, k.Initiative.Set(ctx, 77, types.Initiative{
			Id:        77,
			ProjectId: projectID,
			Status:    types.InitiativeStatus_INITIATIVE_STATUS_CLOSED,
		}))
		_, err = ms.CancelProject(ctx, &types.MsgCancelProject{Creator: creatorStr, ProjectId: projectID, Reason: "done with it"})
		require.NoError(t, err)

		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_CANCELLED, project.Status)
	})
}

// cancelGate makes every address an individual Operations Committee member
// (the fixture default) but only `policy` the technical council's Operations
// Committee policy, so the tests can tell a single member from the committee.
func cancelGate(t *testing.T) (*fixture, string, string) {
	t.Helper()
	f := initFixture(t)
	policy, err := f.addressCodec.BytesToString(sdk.AccAddress([]byte("tech_ops_policy_")))
	require.NoError(t, err)
	member, err := f.addressCodec.BytesToString(sdk.AccAddress([]byte("ops-member")))
	require.NoError(t, err)
	f.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(_ context.Context, addr, council, committee string) bool {
		return addr == policy && council == "technical" && committee == "operations"
	}
	return f, policy, member
}
