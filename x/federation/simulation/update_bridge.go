package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgUpdateBridge delivers MsgUpdateBridge (a new endpoint for a
// non-suspended binding) from a Commons Operations Committee member, which
// the handler accepts on its own, and is a NoOp when no sim account holds a
// seat.
func SimulateMsgUpdateBridge(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUpdateBridge{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpdateBridge{}
		signer, ok := pick(e.r, e.opsCommittee())
		if !ok {
			return noOp(msg, "no sim account is a Commons Operations Committee member")
		}
		bindings, err := e.bindingsWhere(func(b types.BridgeBinding) bool { return !b.Suspended })
		if err != nil {
			return noOpErr(msg, err)
		}
		b, ok := pick(e.r, bindings)
		if !ok {
			return noOp(msg, "no non-suspended bridge binding")
		}
		msg.Authority, msg.Operator, msg.PeerId, msg.Endpoint = signer.Address.String(), b.Address, b.PeerId, randomEndpoint(e.r)
		return e.submit(signer, msg, math.ZeroInt())
	})
}
