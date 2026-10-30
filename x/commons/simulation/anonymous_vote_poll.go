package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgAnonymousVotePoll is not simulated: the message runs only
// inside x/shield's MsgShieldedExec with a Groth16 proof against the poll's
// voter snapshot, which the simulation cannot produce. Keeper, shield and
// e2e tests cover it.
func SimulateMsgAnonymousVotePoll(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, func(*simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgAnonymousVotePoll{}, "routed through x/shield MsgShieldedExec with a ZK proof")
	})
}
