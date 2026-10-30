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

func TestMsgServerApproveProjectBudget(t *testing.T) {
	t.Run("invalid approver address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ApproveProjectBudget(f.ctx, &types.MsgApproveProjectBudget{
			Approver:       "invalid-address",
			ProjectId:      1,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1000)),
			ApprovedSpark:  keeper.PtrInt(math.NewInt(0)),
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid approver address")
	})

	t.Run("non-existent project", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		approver := sdk.AccAddress([]byte("approver"))
		approverStr, err := f.addressCodec.BytesToString(approver)
		require.NoError(t, err)

		_, err = ms.ApproveProjectBudget(f.ctx, &types.MsgApproveProjectBudget{
			Approver:       approverStr,
			ProjectId:      99999,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1000)),
			ApprovedSpark:  keeper.PtrInt(math.NewInt(0)),
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "project not found")
	})

	t.Run("authorized_as_committee_member", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project
		// In tests, mock commons keeper returns true for IsCommitteeMember.
		// So checking for unauthorized error is not easy unless we can modify the mock.
		// Instead, we verify that as a deemed committee member, it SUCCEEDS.

		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)

		approver := sdk.AccAddress([]byte("approver"))
		approverStr, err := f.addressCodec.BytesToString(approver)
		require.NoError(t, err)

		// This should succeed because mock authentication passes
		_, err = ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
			Approver:       approverStr,
			ProjectId:      projectID,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1000)),
			ApprovedSpark:  keeper.PtrInt(math.NewInt(0)),
		})
		require.NoError(t, err)
	})

	t.Run("large budget rejected for plain committee member", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Set a low threshold for testing: 500 micro-DREAM
		params, _ := k.Params.Get(ctx)
		params.LargeProjectBudgetThreshold = math.NewInt(500)
		require.NoError(t, k.Params.Set(ctx, params))

		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)

		approver := sdk.AccAddress([]byte("approver"))
		approverStr, _ := f.addressCodec.BytesToString(approver)

		// Mock: approver IS a committee member but NOT a council policy address
		// Default fixture: IsCommitteeMember→true, IsCouncilAuthorized→false (nil fn)
		// The msg_server should reject because budget (1000) > threshold (500) and
		// the approver is a plain committee member, not a policy address.
		f.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(context.Context, string, string, string) bool { return false }

		_, err := ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
			Approver:       approverStr,
			ProjectId:      projectID,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1000)), // > 500 threshold
			ApprovedSpark:  keeper.PtrInt(math.NewInt(0)),
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrLargeProjectNeedsCouncil)
	})

	t.Run("large budget accepted from council policy address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		params, _ := k.Params.Get(ctx)
		params.LargeProjectBudgetThreshold = math.NewInt(500)
		require.NoError(t, k.Params.Set(ctx, params))

		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)

		policyAddr := sdk.AccAddress([]byte("council_policy_addr"))
		policyStr, _ := f.addressCodec.BytesToString(policyAddr)

		// Mock: policy address is NOT a committee member (it's a policy address, not a person)
		// but IS council-authorized (governance/policy level).
		// IsOperationsCommittee in the keeper also checks IsCommitteeMember — for policy
		// addresses in production, the keeper recognizes them. We simulate this by
		// returning true from IsCommitteeMember only for the keeper's check on the
		// specific council. Since both msg_server and keeper call the same mock, we
		// use IsCouncilAuthorized to let the policy address through the keeper too.
		f.commonsKeeper.IsCommitteeMemberFn = func(_ context.Context, addr sdk.AccAddress, _ string, _ string) (bool, error) {
			return false, nil // Policy addresses are not personal committee members
		}
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, addr string, _ string, _ string) bool {
			return addr == policyStr
		}

		_, err := ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
			Approver:       policyStr,
			ProjectId:      projectID,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1000)), // > 500 threshold
			ApprovedSpark:  keeper.PtrInt(math.NewInt(0)),
		})
		require.NoError(t, err)
	})

	t.Run("small budget still works for committee member", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		params, _ := k.Params.Get(ctx)
		params.LargeProjectBudgetThreshold = math.NewInt(500)
		require.NoError(t, k.Params.Set(ctx, params))

		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)

		approver := sdk.AccAddress([]byte("approver"))
		approverStr, _ := f.addressCodec.BytesToString(approver)

		// Budget = 400 <= threshold 500 → plain committee member should succeed
		_, err := ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
			Approver:       approverStr,
			ProjectId:      projectID,
			ApprovedBudget: keeper.PtrInt(math.NewInt(400)), // <= 500 threshold
			ApprovedSpark:  keeper.PtrInt(math.NewInt(0)),
		})
		require.NoError(t, err)
	})

	t.Run("successful approval", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup
		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)

		approver := sdk.AccAddress([]byte("approver"))
		approverStr, err := f.addressCodec.BytesToString(approver)
		require.NoError(t, err)

		// Mock committee membership (handled by fixture)

		_, err = ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
			Approver:       approverStr,
			ProjectId:      projectID,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1000)),
			ApprovedSpark:  keeper.PtrInt(math.NewInt(0)),
		})
		require.NoError(t, err)

		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, math.NewInt(1000), keeper.DerefInt(project.ApprovedBudget))
	})

	// Regression test for the cross-council approval tightening: an
	// operations-committee member of council A can no longer unilaterally
	// approve a project pointed at council B. Previously the global
	// IsOperationsCommittee helper would return true for either Technical or
	// Commons ops members and the keeper's own check accepted that, letting
	// any ops-committee member approve any project regardless of which council
	// the project named.
	t.Run("cross-council approval rejected", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator"))
		// Project is pointed at the commons council.
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "commons", math.NewInt(10000), math.NewInt(1000), false)

		approver := sdk.AccAddress([]byte("approver"))
		approverStr, _ := f.addressCodec.BytesToString(approver)

		// Mock: approver is on the technical ops committee but NOT on the
		// commons ops committee. Default IsCouncilAuthorizedFn returns false
		// (no policy-address fallback).
		f.commonsKeeper.IsCommitteeMemberFn = func(_ context.Context, _ sdk.AccAddress, council string, _ string) (bool, error) {
			return council == "technical", nil
		}

		_, err := ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
			Approver:       approverStr,
			ProjectId:      projectID,
			ApprovedBudget: keeper.PtrInt(math.NewInt(1000)),
			ApprovedSpark:  keeper.PtrInt(math.NewInt(0)),
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	// The Operations Committee's policy (an executed committee proposal) is
	// not a personal committee member; it must be able to approve a budget
	// on either side of the large-project threshold, and so must gov.
	t.Run("committee policy approves small and large budgets", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		params, _ := k.Params.Get(ctx)
		params.LargeProjectBudgetThreshold = math.NewInt(500)
		require.NoError(t, k.Params.Set(ctx, params))

		policyStr, err := f.addressCodec.BytesToString(sdk.AccAddress([]byte("tech_ops_policy_")))
		require.NoError(t, err)
		f.commonsKeeper.IsCommitteeMemberFn = func(context.Context, sdk.AccAddress, string, string) (bool, error) {
			return false, nil
		}
		f.commonsKeeper.IsCouncilAuthorizedFn = func(context.Context, string, string, string) bool { return false }
		f.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(_ context.Context, addr, council, committee string) bool {
			return addr == policyStr && council == "technical" && committee == "operations"
		}

		creator := sdk.AccAddress([]byte("creator"))
		for _, budget := range []int64{400, 1000} {
			projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
			require.NoError(t, err)
			_, err = ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
				Approver:       policyStr,
				ProjectId:      projectID,
				ApprovedBudget: keeper.PtrInt(math.NewInt(budget)),
				ApprovedSpark:  keeper.PtrInt(math.NewInt(0)),
			})
			require.NoError(t, err, "budget %d", budget)
			project, err := k.GetProject(ctx, projectID)
			require.NoError(t, err)
			require.Equal(t, types.ProjectStatus_PROJECT_STATUS_ACTIVE, project.Status)
		}

		// Gov approves a small budget too (it is not a committee member).
		gov, err := f.addressCodec.BytesToString(k.GetAuthority())
		require.NoError(t, err)
		projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		require.NoError(t, err)
		_, err = ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
			Approver:       gov,
			ProjectId:      projectID,
			ApprovedBudget: keeper.PtrInt(math.NewInt(400)),
			ApprovedSpark:  keeper.PtrInt(math.NewInt(0)),
		})
		require.NoError(t, err)

		// The policy of another council's committee is not this project's body.
		otherID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "ecosystem", math.NewInt(10000), math.NewInt(1000), false)
		require.NoError(t, err)
		_, err = ms.ApproveProjectBudget(ctx, &types.MsgApproveProjectBudget{
			Approver:       policyStr,
			ProjectId:      otherID,
			ApprovedBudget: keeper.PtrInt(math.NewInt(400)),
			ApprovedSpark:  keeper.PtrInt(math.NewInt(0)),
		})
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})
}
