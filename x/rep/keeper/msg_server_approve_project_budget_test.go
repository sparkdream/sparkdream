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
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))

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
		k.Params.Set(ctx, params)

		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))

		approver := sdk.AccAddress([]byte("approver"))
		approverStr, _ := f.addressCodec.BytesToString(approver)

		// Mock: approver IS a committee member but NOT a council policy address
		// Default fixture: IsCommitteeMember→true, IsCouncilAuthorized→false (nil fn)
		// The msg_server should reject because budget (1000) > threshold (500) and
		// the approver is a plain committee member, not a policy address.

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
		k.Params.Set(ctx, params)

		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))

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
		k.Params.Set(ctx, params)

		creator := sdk.AccAddress([]byte("creator"))
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))

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
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))

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
}
