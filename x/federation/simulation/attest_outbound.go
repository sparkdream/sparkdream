package simulation

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgAttestOutbound delivers MsgAttestOutbound from a working bridge
// binding (registering one first when none exists) for an outbound content
// type of the peer policy. A NoOp when there is no ACTIVE bridged peer or
// the policy allows no outbound type.
func SimulateMsgAttestOutbound(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgAttestOutbound{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAttestOutbound{}
		b, ok, err := e.ensureBinding(func(b types.BridgeBinding) bool {
			pol, found := e.policy(b.PeerId)
			return found && len(pol.OutboundContentTypes) > 0
		}, func(p types.Peer) bool {
			pol, found := e.policy(p.Id)
			return found && len(pol.OutboundContentTypes) > 0
		}, anyone)
		if err != nil {
			return noOpErr(msg, err)
		}
		if !ok {
			return noOp(msg, "no working bridge binding on a peer with outbound content types")
		}
		acc, ok := e.account(b.Address)
		if !ok {
			return noOp(msg, "bridge operator is not a live sim account")
		}
		pol, _ := e.policy(b.PeerId)
		ct, _ := pick(e.r, pol.OutboundContentTypes)
		msg.Operator, msg.PeerId, msg.ContentType = b.Address, b.PeerId, ct
		msg.LocalContentId = fmt.Sprintf("%s/%d", ct, e.r.Intn(100_000))
		return e.submit(acc, msg, math.ZeroInt())
	})
}
