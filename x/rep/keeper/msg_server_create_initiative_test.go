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
		resp, err := ms.CreateInitiative(ctx, &types.MsgCreateInitiative{
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

		// Authorship is recorded on state, not only in the creation event
		initiative, err := k.GetInitiative(ctx, resp.InitiativeId)
		require.NoError(t, err)
		require.Equal(t, creatorStr, initiative.Creator)
	})
}

// A project's approved_budget is a ceiling a council voted for, and
// CreateInitiative draws against it via AllocateBudget — which checks only that
// the project is ACTIVE and has room. Without an ownership gate any member could
// commission work against somebody else's council-approved budget until it was
// exhausted.
//
// These cases all pass a narrow authorization policy: the default fixture policy
// is AlwaysAuthorized, under which every caller reads as Operations Committee and
// the gate can never reject.
func TestCreateInitiativeRequiresProjectStanding(t *testing.T) {
	// setup returns a fixture whose only committee member is `opsMember`, plus an
	// approved budget-backed project owned by `owner`.
	setup := func(t *testing.T, opsMember sdk.AccAddress) (*fixture, keeper.Keeper, sdk.AccAddress, uint64) {
		t.Helper()
		f := initFixture(t, WithAuthorizationPolicy(
			func(addr sdk.AccAddress, _ string, _ string) bool { return addr.Equals(opsMember) },
		))
		k := f.keeper

		owner := sdk.AccAddress([]byte("f6-owner-address-"))
		mkFundedMember(t, k, f.ctx, owner, 10_000_000)
		projectID, err := k.CreateProject(f.ctx, owner, "Funded", "Desc", []string{"tag"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.NewInt(10000), math.NewInt(1000), false)
		require.NoError(t, err)
		// Approve as opsMember: the narrow policy above rejects everyone else, and
		// ApproveProject is itself committee-gated, so a generic "approver"
		// address fails setup before the test under examination even runs.
		require.NoError(t, k.ApproveProject(f.ctx, projectID, opsMember,
			math.NewInt(10000), math.NewInt(1000)))
		return f, k, owner, projectID
	}

	newMsg := func(t *testing.T, f *fixture, from sdk.AccAddress, projectID uint64) *types.MsgCreateInitiative {
		t.Helper()
		s, err := f.addressCodec.BytesToString(from)
		require.NoError(t, err)
		return &types.MsgCreateInitiative{
			Creator: s, ProjectId: projectID, Title: "Task", Tags: []string{"tag"},
			Tier: 1, Category: 1, Budget: keeper.PtrInt(math.NewInt(100)),
		}
	}

	t.Run("an outsider cannot spend a budget-backed project's ceiling", func(t *testing.T) {
		opsMember := sdk.AccAddress([]byte("f6-ops-member-adr"))
		f, k, _, projectID := setup(t, opsMember)
		ms := keeper.NewMsgServerImpl(f.keeper)

		outsider := sdk.AccAddress([]byte("f6-outsider-addr-"))
		mkFundedMember(t, k, f.ctx, outsider, 10_000_000)

		_, err := ms.CreateInitiative(f.ctx, newMsg(t, f, outsider, projectID))
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUnauthorized)

		// The budget must be untouched — the point of the gate is the allocation,
		// not merely the initiative record.
		project, err := k.GetProject(f.ctx, projectID)
		require.NoError(t, err)
		require.True(t, keeper.DerefInt(project.AllocatedBudget).IsZero())
	})

	t.Run("the project creator can", func(t *testing.T) {
		opsMember := sdk.AccAddress([]byte("f6-ops-member-adr"))
		f, k, owner, projectID := setup(t, opsMember)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CreateInitiative(f.ctx, newMsg(t, f, owner, projectID))
		require.NoError(t, err)

		project, err := k.GetProject(f.ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, "100", keeper.DerefInt(project.AllocatedBudget).String())
	})

	t.Run("the operations committee can", func(t *testing.T) {
		opsMember := sdk.AccAddress([]byte("f6-ops-member-adr"))
		f, k, _, projectID := setup(t, opsMember)
		ms := keeper.NewMsgServerImpl(f.keeper)
		mkFundedMember(t, k, f.ctx, opsMember, 10_000_000)

		_, err := ms.CreateInitiative(f.ctx, newMsg(t, f, opsMember, projectID))
		require.NoError(t, err, "the committee keeps its administrative escape hatch")
	})

	t.Run("permissionless projects stay open to anyone", func(t *testing.T) {
		opsMember := sdk.AccAddress([]byte("f6-ops-member-adr"))
		f, k, _, _ := setup(t, opsMember)
		ms := keeper.NewMsgServerImpl(f.keeper)

		owner := sdk.AccAddress([]byte("f6-perm-owner-adr"))
		mkFundedMember(t, k, f.ctx, owner, 10_000_000)
		openID, err := k.CreateProject(f.ctx, owner, "Open", "Desc", []string{"tag"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.ZeroInt(), math.ZeroInt(), true)
		require.NoError(t, err)

		outsider := sdk.AccAddress([]byte("f6-perm-outsider-"))
		mkFundedMember(t, k, f.ctx, outsider, 10_000_000)

		_, err = ms.CreateInitiative(f.ctx, newMsg(t, f, outsider, openID))
		require.NoError(t, err, "open contribution is the entire point of permissionless mode")
	})

	t.Run("non-membership still reports as non-membership", func(t *testing.T) {
		// Ordering matters: the E2E suite asserts on this error for a non-member,
		// so the ownership gate must not preempt the membership check.
		opsMember := sdk.AccAddress([]byte("f6-ops-member-adr"))
		f, _, _, projectID := setup(t, opsMember)
		ms := keeper.NewMsgServerImpl(f.keeper)

		stranger := sdk.AccAddress([]byte("f6-stranger-addr-"))
		_, err := ms.CreateInitiative(f.ctx, newMsg(t, f, stranger, projectID))
		require.Error(t, err)
		require.Contains(t, err.Error(), "creator must be a member")
	})
}
