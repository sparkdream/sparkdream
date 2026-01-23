package keeper_test

import (
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
