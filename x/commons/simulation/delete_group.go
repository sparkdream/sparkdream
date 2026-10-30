package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// minSimGroupsKept is how many simulated groups DeleteGroup leaves standing,
// so the ops that need a target keep finding one.
const minSimGroupsKept = 2

// SimulateMsgDeleteGroup deletes a simulated group through a proposal of its
// parent or of the parent's veto policy (a sibling policy, which the
// handler also accepts). Bootstrapped bodies are never deleted.
func SimulateMsgDeleteGroup(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgDeleteGroup{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, func() (proposal, string, error) {
			targets, err := e.groups(func(name string, _ types.Group) bool { return isSimGroup(name) })
			if err != nil {
				return proposal{}, "", err
			}
			if len(targets) <= minSimGroupsKept {
				return proposal{}, "too few simulated groups to delete one", nil
			}
			target := targets[e.r.Intn(len(targets))]
			candidates, err := e.parentAndVeto(target.g)
			if err != nil {
				return proposal{}, "", err
			}
			policy, proposer, reason, err := e.pickCarrier(candidates, sdk.MsgTypeURL(label))
			if err != nil || reason != "" {
				return proposal{}, reason, err
			}
			return proposal{policy: policy, proposer: proposer, inner: &types.MsgDeleteGroup{
				Authority: policy,
				GroupName: target.name,
			}}, "", nil
		})
	})
}

// parentAndVeto returns g's parent policy and the parent body's veto policy:
// the two council signers MsgDeleteGroup and MsgVetoGroupProposals accept.
func (e *simEnv) parentAndVeto(g types.Group) ([]string, error) {
	out := []string{g.ParentPolicyAddress}
	parent, ok, err := e.policyBody(g.ParentPolicyAddress)
	if err != nil || !ok {
		return out, err
	}
	veto, err := e.vetoPolicy(parent.name)
	if err != nil || veto == "" {
		return out, err
	}
	return append(out, veto), nil
}
