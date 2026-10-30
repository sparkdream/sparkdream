package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// interimAssigneeCandidates returns one candidate per live assignee of the
// given interims, built by mk.
func (e *simEnv) interimAssigneeCandidates(its []types.Interim, mk func(acc simtypes.Account, it types.Interim) *candidate) []candidate {
	var out []candidate
	for _, it := range its {
		for _, a := range it.Assignees {
			if acc, ok := e.account(a); ok {
				if c := mk(acc, it); c != nil {
					out = append(out, *c)
				}
			}
		}
	}
	return out
}

// SimulateMsgSubmitInterimWork has an assignee of an open interim submit its
// deliverable. With none, a member opens one first.
func SimulateMsgSubmitInterimWork(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgSubmitInterimWork{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		its, err := e.openInterim(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		cands := e.interimAssigneeCandidates(its, func(acc simtypes.Account, it types.Interim) *candidate {
			return &candidate{acc: acc, msg: &types.MsgSubmitInterimWork{
				Creator: acc.Address.String(), InterimId: it.Id, DeliverableUri: randomURI(e.r), Comments: "simulation",
			}, spent: math.ZeroInt()}
		})
		return e.firstValid(label, cands, 4, "no open interim with a live assignee")
	})
}
