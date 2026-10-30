package simulation

import (
	"slices"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgCreatePolicyPermissions is not simulated. The handler accepts
// x/gov or the policy itself, and a policy signs only through an executed
// proposal of its own body, which SubmitProposal refuses while the policy
// has no allowlist: self-registration is unreachable, and only x/gov can
// create an allowlist.
func SimulateMsgCreatePolicyPermissions(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, func(*simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgCreatePolicyPermissions{},
			"x/gov only: a policy without an allowlist cannot submit the proposal that would sign it")
	})
}

// SimulateMsgUpdatePolicyPermissions narrows a simulated group's own
// allowlist through a proposal of that group (self-regulation may only
// remove messages). It drops one message other than
// MsgUpdatePolicyPermissions itself, or resubmits the list unchanged when
// nothing else is left. Bootstrapped bodies keep their allowlists: every
// other module's simulation depends on them.
func SimulateMsgUpdatePolicyPermissions(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUpdatePolicyPermissions{}
	typeURL := sdk.MsgTypeURL(label)
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, func() (proposal, string, error) {
			targets, err := e.groups(func(name string, _ types.Group) bool { return isSimGroup(name) })
			if err != nil {
				return proposal{}, "", err
			}
			var policies []string
			for _, t := range targets {
				policies = append(policies, t.g.PolicyAddress)
			}
			policy, proposer, reason, err := e.pickCarrier(policies, typeURL)
			if err != nil || reason != "" {
				if reason != "" {
					reason = "no simulated group can narrow its own allowlist: " + reason
				}
				return proposal{}, reason, err
			}
			perms, err := e.k.PolicyPermissions.Get(e.ctx, policy)
			if err != nil {
				return proposal{}, "", err
			}
			allowed := slices.Clone(perms.AllowedMessages)
			var droppable []int
			for i, m := range allowed {
				if m != typeURL {
					droppable = append(droppable, i)
				}
			}
			if i, ok := pick(e.r, droppable); ok {
				allowed = slices.Delete(allowed, i, i+1)
			}
			return proposal{policy: policy, proposer: proposer, inner: &types.MsgUpdatePolicyPermissions{
				Authority:       policy,
				PolicyAddress:   policy,
				AllowedMessages: allowed,
			}}, "", nil
		})
	})
}

// SimulateMsgDeletePolicyPermissions is not simulated: the policy itself or
// x/gov must sign it, and no body holds MsgDeletePolicyPermissions (none of
// the bootstrapped allowlists has it, and a council can only delegate what
// it holds), so only x/gov can delete an allowlist.
func SimulateMsgDeletePolicyPermissions(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, func(*simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgDeletePolicyPermissions{},
			"x/gov only: no body's allowlist holds MsgDeletePolicyPermissions")
	})
}
