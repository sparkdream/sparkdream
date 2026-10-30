package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// SimulateMsgReject rejects a proposed contribution through a Commons
// Council proposal (council.go), like SimulateMsgApprove. New rejection
// proposals are opened only one call in three, so most proposed
// contributions are left for approval and the rest of the lifecycle.
func SimulateMsgReject(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgReject{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		props, err := e.revealProposals()
		if err != nil {
			return noOpErr(&types.MsgReject{}, err)
		}
		open := false
		for _, cp := range props {
			if cp.kind == kindReject {
				open = true
				break
			}
		}
		if !open && e.r.Intn(3) != 0 {
			return noOp(&types.MsgReject{}, "leaving proposed contributions to approval this time")
		}
		opMsg, _, err := e.advanceCouncil(kindReject)
		return opMsg, nil, err
	})
}
