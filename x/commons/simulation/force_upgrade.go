package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgForceUpgrade is not simulated. Only the Technical Council holds
// MsgForceUpgrade (a restricted message), and a scheduled upgrade halts the
// chain at its height: the simulator has no upgrade handler for it, so the
// run would stop there. Keeper and e2e tests cover the path.
func SimulateMsgForceUpgrade(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, func(*simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgForceUpgrade{}, "a scheduled upgrade halts the simulation at its height")
	})
}
