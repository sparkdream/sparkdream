package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgSetForumPaused delivers MsgSetForumPaused from a sim account
// that is a Commons Operations Committee member (the only non-policy signer
// the handler accepts) and is a NoOp when none is. A paused forum is always
// unpaused; an unpaused one is paused rarely, so the rest of the forum
// simulation keeps running.
func SimulateMsgSetForumPaused(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgSetForumPaused{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSetForumPaused{}
		signer, ok := pick(e.r, e.opsCommittee())
		if !ok {
			return noOp(msg, "no sim account is a Commons Operations Committee member")
		}
		if !e.params.ForumPaused && e.r.Intn(5) != 0 {
			return noOp(msg, "leaving the forum unpaused")
		}
		msg.Creator, msg.Paused = signer.Address.String(), !e.params.ForumPaused
		return e.submit(signer, msg, math.ZeroInt())
	})
}
