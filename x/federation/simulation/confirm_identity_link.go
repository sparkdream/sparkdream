package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgConfirmIdentityLink is always a NoOp: it answers a pending
// identity challenge that only arrives in an IBC packet from a Spark Dream
// peer, and sends the confirmation back over IBC. The simulation has no
// IBC channel.
func SimulateMsgConfirmIdentityLink(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgConfirmIdentityLink{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgConfirmIdentityLink{}, "IBC packet path: no IBC channel in the simulation")
	})
}
