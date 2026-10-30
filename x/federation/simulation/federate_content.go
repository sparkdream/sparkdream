package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgFederateContent is always a NoOp: MsgFederateContent sends an
// IBC content packet to an ACTIVE Spark Dream peer, and the simulation has
// no IBC channel (and no policy address to activate such a peer).
func SimulateMsgFederateContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgFederateContent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgFederateContent{}, "IBC packet path: no IBC channel in the simulation")
	})
}
