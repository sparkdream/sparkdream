package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgEmergencyCancelGovProposal is not simulated. The Commons
// Council's veto policy could carry it, but the SDK gov simulation schedules
// future MsgVote operations on every proposal it submits, and a vote on a
// proposal this op cancelled is a delivered tx the gov handler rejects,
// which fails the whole simulation. Keeper and e2e tests cover the path.
func SimulateMsgEmergencyCancelGovProposal(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, func(*simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgEmergencyCancelGovProposal{},
			"cancelling an x/gov proposal fails the gov simulation's scheduled votes on it")
	})
}
