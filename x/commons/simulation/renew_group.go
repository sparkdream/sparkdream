package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgRenewGroup renews a simulated group whose term has expired
// (they get terms of one to five days) with a fresh membership, through a
// proposal of its parent or electoral authority. Only simulated groups are
// renewed: the bootstrapped bodies have terms far beyond the run.
func SimulateMsgRenewGroup(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRenewGroup{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, func() (proposal, string, error) {
			expired, err := e.groups(func(name string, g types.Group) bool { return isSimGroup(name) && e.termExpired(g) })
			if err != nil {
				return proposal{}, "", err
			}
			if len(expired) == 0 {
				return proposal{}, "no simulated group's term has expired", nil
			}
			reason := ""
			for _, i := range e.r.Perm(len(expired)) {
				target := expired[i]
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
				members, why := e.randomMembership(target.g)
				if why != "" {
					reason = why
					continue
				}
				weights := make([]string, len(members))
				for j := range weights {
					weights[j] = "1"
				}
				return proposal{policy: policy, proposer: proposer, inner: &types.MsgRenewGroup{
					Authority:        policy,
					GroupName:        target.name,
					NewMembers:       members,
					NewMemberWeights: weights,
				}}, "", nil
			}
			return proposal{}, reason, nil
		})
	})
}

// randomMembership draws a membership of live sim accounts sized within the
// group's bounds.
func (e *simEnv) randomMembership(g types.Group) ([]string, string) {
	live := e.liveAccounts()
	lo := commontypes.Saturate[int](max(g.MinMembers, 1))
	hi := min(commontypes.Saturate[int](g.MaxMembers), len(live), 6)
	if hi < lo {
		return nil, "not enough live sim accounts for the group's minimum size"
	}
	n := lo + e.r.Intn(hi-lo+1)
	out := make([]string, 0, n)
	for _, i := range e.r.Perm(len(live))[:n] {
		out = append(out, live[i].Address.String())
	}
	return out, ""
}
