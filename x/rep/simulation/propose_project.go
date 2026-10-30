package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgProposeProject has a member propose a project: permissionless
// (no budget, creation fee burned) when its trust level allows and the coin
// says so, otherwise a budget-backed proposal for a council to approve.
func SimulateMsgProposeProject(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgProposeProject{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ms, err := e.members(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		cands := make([]candidate, 0, len(ms))
		for _, ma := range ms {
			msg, err := e.projectMsg(ma, e.canPermissionless(ma) && e.r.Intn(2) == 0)
			if err != nil {
				return noOpErr(label, err)
			}
			cands = append(cands, candidate{acc: ma.acc, msg: msg, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 6, "no member can propose a project")
	})
}
