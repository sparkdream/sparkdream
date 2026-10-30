package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgRequestReputationAttestation is always a NoOp: the request is
// an IBC reputation-query packet to a Spark Dream peer over a VERIFIED
// identity link (verified only by an IBC confirmation), and the simulation
// has no IBC channel.
func SimulateMsgRequestReputationAttestation(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgRequestReputationAttestation{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(&types.MsgRequestReputationAttestation{}, "IBC packet path: no IBC channel in the simulation")
	})
}
