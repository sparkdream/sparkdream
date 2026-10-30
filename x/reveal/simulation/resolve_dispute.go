package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// SimulateMsgResolveDispute resolves a DISPUTED tranche with a random
// verdict through a Commons Council proposal (council.go). Disputes only
// arise in the EndBlocker, when a verification deadline tallies too few
// YES votes, so the op is a NoOp until one exists.
func SimulateMsgResolveDispute(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgResolveDispute{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		opMsg, _, err := e.advanceCouncil(kindResolve)
		return opMsg, nil, err
	})
}
