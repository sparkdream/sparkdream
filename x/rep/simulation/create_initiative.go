package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCreateInitiative creates an initiative under an ACTIVE project,
// signed by the project's creator, by any member for a permissionless
// project, or by an Operations Committee seat. With no ACTIVE project one is
// opened first (a permissionless proposal, or a budget-backed one approved
// by a committee seat).
func SimulateMsgCreateInitiative(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCreateInitiative{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		cands, err := e.createInitiativeCandidates()
		if err != nil {
			return noOpErr(label, err)
		}
		if len(cands) == 0 {
			ok, err := e.ensureActiveProject()
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok {
				return noOp(label, "no ACTIVE project and none could be opened")
			}
			if cands, err = e.createInitiativeCandidates(); err != nil {
				return noOpErr(label, err)
			}
		}
		return e.firstValid(label, cands, 6, "no member may create an initiative under an ACTIVE project")
	})
}
