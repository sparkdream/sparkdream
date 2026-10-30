package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgVetoGroupProposals vetoes a simulated group's pending proposals
// (bumps its policy version) through a fee-exempt proposal of its parent's
// veto policy (or the parent itself when that holds the message). It
// prefers a group with open proposals, so the veto has something to stop.
// Bootstrapped bodies are never vetoed: other modules' simulations have
// proposals in flight on them.
func SimulateMsgVetoGroupProposals(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgVetoGroupProposals{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, func() (proposal, string, error) {
			targets, err := e.groups(func(name string, _ types.Group) bool { return isSimGroup(name) })
			if err != nil {
				return proposal{}, "", err
			}
			if len(targets) == 0 {
				return proposal{}, "no simulated group", nil
			}
			pending, err := e.policiesWithOpenProposals()
			if err != nil {
				return proposal{}, "", err
			}
			busy := make([]namedGroup, 0, len(targets))
			for _, t := range targets {
				if pending[t.g.PolicyAddress] {
					busy = append(busy, t)
				}
			}
			if len(busy) > 0 {
				targets = busy
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
			return proposal{policy: policy, proposer: proposer, inner: &types.MsgVetoGroupProposals{
				Authority: policy,
				GroupName: target.name,
			}}, "", nil
		})
	})
}

// policiesWithOpenProposals returns the policies with open proposals.
func (e *simEnv) policiesWithOpenProposals() (map[string]bool, error) {
	out := map[string]bool{}
	ids, err := e.openProposalIDs()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		p, err := e.k.Proposals.Get(e.ctx, id)
		if err != nil {
			return nil, err
		}
		out[p.PolicyAddress] = true
	}
	return out, nil
}
