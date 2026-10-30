package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgSubmitInitiativeWork has the assignee of an ASSIGNED initiative
// submit a deliverable. With none, an OPEN initiative is assigned first.
func SimulateMsgSubmitInitiativeWork(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgSubmitInitiativeWork{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		in, acc, ok, err := e.assignedInitiative(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no ASSIGNED initiative with a live assignee")
		}
		msg := submitMsg(acc, in.Id)
		msg.DeliverableUri = randomURI(e.r)
		return e.submit(acc, msg, math.ZeroInt())
	})
}
