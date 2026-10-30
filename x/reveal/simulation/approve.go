package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// SimulateMsgApprove approves a proposed contribution. MsgApprove is signed
// only by the Commons Council policy (or x/gov), so the op drives the
// council proposal that carries it one step (council.go): execute an
// accepted approval, vote an open one through, or submit a new one for a
// proposed contribution (proposing the contribution first when none is
// waiting). The delivered x/commons message is reported.
func SimulateMsgApprove(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgApprove{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		opMsg, _, err := e.advanceCouncil(kindApprove)
		return opMsg, nil, err
	})
}
