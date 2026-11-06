package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// Deleting a group removes its x/split share, so the community pool stops
// paying a policy address nothing can spend from.
func TestDeleteGroup_RemovesSplitShare(t *testing.T) {
	k, ctx, _, split := setupCommonsKeeperWithSplit(t)
	ms := keeper.NewMsgServerImpl(k)

	parent := sdk.AccAddress([]byte("parent_addr_________")).String()
	policy := keeper.DeriveCouncilAddress(1, "standard").String()
	require.NoError(t, k.Groups.Set(ctx, "Aurora Guild", types.Group{
		GroupId: 1, PolicyAddress: policy, ParentPolicyAddress: parent, FundingWeight: 10,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, policy, "Aurora Guild"))
	require.NoError(t, split.SetShareByAddress(ctx, policy, 10))

	_, err := ms.DeleteGroup(ctx, &types.MsgDeleteGroup{Authority: parent, GroupName: "Aurora Guild"})
	require.NoError(t, err)
	require.NotContains(t, split.shares, policy)
}

// An accepted proposal stops executing once its policy loses the permission
// for one of its messages, or loses its permissions altogether.
func TestExecuteProposal_RechecksPermissions(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)

	policy := sdk.AccAddress([]byte("policy_perm_test____")).String()
	executor := sdk.AccAddress([]byte("executor____________")).String()
	require.NoError(t, k.Groups.Set(ctx, "Phoenix Council", types.Group{
		PolicyAddress: policy, CurrentTermExpiration: ctx.BlockTime().Unix() + 86400,
	}))
	msgType := sdk.MsgTypeURL(&types.MsgUpdateParams{})
	proposal := func(id uint64) {
		require.NoError(t, k.Proposals.Set(ctx, id, types.Proposal{
			Id: id, CouncilName: "Phoenix Council", PolicyAddress: policy,
			Status:        types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED,
			ExecutionTime: ctx.BlockTime().Unix() - 10,
			Messages:      []*codectypes.Any{packMsg(t, &types.MsgUpdateParams{Authority: policy, Params: types.DefaultParams()})},
		}))
	}

	// Narrowed: the message type is no longer allowed.
	require.NoError(t, k.PolicyPermissions.Set(ctx, policy, types.PolicyPermissions{
		PolicyAddress: policy, AllowedMessages: []string{"/sparkdream.commons.v1.MsgSpendFromCommons"},
	}))
	// The tx succeeds and the proposal ends FAILED (CMN-108: the status
	// persists instead of rolling back with an error).
	failed := func(id uint64) types.Proposal {
		p, err := k.Proposals.Get(ctx, id)
		require.NoError(t, err)
		require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FAILED, p.Status)
		return p
	}
	proposal(1)
	_, err := ms.ExecuteProposal(ctx, &types.MsgExecuteProposal{Executor: executor, ProposalId: 1})
	require.NoError(t, err)
	require.Contains(t, failed(1).FailedReason, "no longer allowed")

	// Deleted: the policy has no permissions at all.
	require.NoError(t, k.PolicyPermissions.Remove(ctx, policy))
	proposal(2)
	_, err = ms.ExecuteProposal(ctx, &types.MsgExecuteProposal{Executor: executor, ProposalId: 2})
	require.NoError(t, err)
	failed(2)

	// Still allowed: the check passes (execution then stops at the unwired
	// router, which this keeper has none of).
	require.NoError(t, k.PolicyPermissions.Set(ctx, policy, types.PolicyPermissions{
		PolicyAddress: policy, AllowedMessages: []string{msgType},
	}))
	proposal(3)
	_, err = ms.ExecuteProposal(ctx, &types.MsgExecuteProposal{Executor: executor, ProposalId: 3})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "no longer allowed")
}
