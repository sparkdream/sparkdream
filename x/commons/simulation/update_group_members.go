package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// maxStandingSeats caps how far the simulation grows a bootstrapped body.
const maxStandingSeats = 7

// SimulateMsgUpdateGroupMembers changes a group's membership through a
// proposal of its parent or electoral authority (for instance the Commons
// Governance Committee electing the Commons Council, or the Commons Council
// staffing its Operations Committee). A simulated group gets a random add,
// removal or swap within its size bounds. A bootstrapped body only ever
// gains a live sim account (up to maxStandingSeats): every other module's
// simulation routes its council-gated messages through those seats.
func SimulateMsgUpdateGroupMembers(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUpdateGroupMembers{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, func() (proposal, string, error) {
			targets, err := e.groups(func(string, types.Group) bool { return true })
			if err != nil {
				return proposal{}, "", err
			}
			reason := "no group"
			for _, i := range e.r.Perm(len(targets)) {
				target := targets[i]
				if _, ok, err := e.policyBody(target.g.PolicyAddress); err != nil || !ok {
					if err != nil {
						return proposal{}, "", err
					}
					continue
				}
				var policy string
				var proposer simtypes.Account
				policy, proposer, reason, err = e.pickCarrier(
					[]string{target.g.ParentPolicyAddress, target.g.ElectoralPolicyAddress}, sdk.MsgTypeURL(label))
				if err != nil {
					return proposal{}, "", err
				}
				if reason != "" {
					continue
				}
				msg, why, err := e.membershipChange(target, policy)
				if err != nil {
					return proposal{}, "", err
				}
				if why != "" {
					reason = why
					continue
				}
				return proposal{policy: policy, proposer: proposer, inner: msg}, "", nil
			}
			return proposal{}, reason, nil
		})
	})
}

// membershipChange builds a membership change of target signed by policy.
func (e *simEnv) membershipChange(target namedGroup, policy string) (*types.MsgUpdateGroupMembers, string, error) {
	members, err := e.k.GetCouncilMembers(e.ctx, target.name)
	if err != nil {
		return nil, "", err
	}
	current := map[string]bool{}
	var removable []string
	for _, m := range members {
		current[m.Address] = true
		if _, ok := getAccount(e.accs, m.Address); ok {
			removable = append(removable, m.Address) // never a policy seat
		}
	}
	addable := accountsWhere(e.liveAccounts(), func(a simtypes.Account) bool { return !current[a.Address.String()] })
	count := len(members)
	lo := commontypes.Saturate[int](target.g.MinMembers)
	hi := commontypes.Saturate[int](target.g.MaxMembers)
	msg := &types.MsgUpdateGroupMembers{Authority: policy, GroupPolicyAddress: target.g.PolicyAddress}

	add := func() {
		acc := addable[e.r.Intn(len(addable))]
		msg.MembersToAdd = append(msg.MembersToAdd, acc.Address.String())
		msg.WeightsToAdd = append(msg.WeightsToAdd, "1")
	}
	remove := func() {
		msg.MembersToRemove = append(msg.MembersToRemove, removable[e.r.Intn(len(removable))])
	}

	if !isSimGroup(target.name) {
		if count >= min(hi, maxStandingSeats) || len(addable) == 0 {
			return nil, "bootstrapped body is full (the simulation only adds to it)", nil
		}
		add()
		return msg, "", nil
	}

	canAdd := count < hi && len(addable) > 0
	canRemove := count > lo && len(removable) > 0
	canSwap := len(addable) > 0 && len(removable) > 0
	switch choice := e.r.Intn(3); {
	case choice == 0 && canAdd:
		add()
	case choice == 1 && canRemove:
		remove()
	case canSwap:
		add()
		remove()
	case canAdd:
		add()
	case canRemove:
		remove()
	default:
		return nil, "no membership change fits the group's bounds", nil
	}
	return msg, "", nil
}
