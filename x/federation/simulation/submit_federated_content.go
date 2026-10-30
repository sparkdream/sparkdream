package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgSubmitFederatedContent delivers MsgSubmitFederatedContent from
// a working bridge binding (registering one first through MsgRegisterBridge
// when none exists): an inbound content type of the peer policy, a
// public-domain license, a content_uri and creator handle on the peer's own
// host, an admitted author, and sometimes a supersede of one of the
// operator's still-pending records. A NoOp when there is no ACTIVE bridged
// peer, the policy admits nothing, or the inbound rate limits are spent.
func SimulateMsgSubmitFederatedContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgSubmitFederatedContent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSubmitFederatedContent{}
		b, ok, err := e.ensureBinding(e.inboundBinding, e.inboundPeer, anyone)
		if err != nil {
			return noOpErr(msg, err)
		}
		if !ok {
			return noOp(msg, "no working bridge binding (needs an ACTIVE bridged peer)")
		}
		acc, ok := e.account(b.Address)
		if !ok {
			return noOp(msg, "bridge operator is not a live sim account")
		}
		msg, reason := e.submitContentMsg(b)
		if reason != "" {
			return noOp(msg, reason)
		}
		return e.submit(acc, msg, math.ZeroInt())
	})
}
