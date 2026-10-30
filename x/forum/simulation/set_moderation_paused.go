package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgSetModerationPaused delivers MsgSetModerationPaused from a sim
// account that is a Commons Operations Committee member and is a NoOp when
// none is. Paused moderation is always resumed; active moderation is paused
// rarely.
func SimulateMsgSetModerationPaused(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgSetModerationPaused{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSetModerationPaused{}
		signer, ok := pick(e.r, e.opsCommittee())
		if !ok {
			return noOp(msg, "no sim account is a Commons Operations Committee member")
		}
		if !e.params.ModerationPaused && e.r.Intn(5) != 0 {
			return noOp(msg, "leaving moderation active")
		}
		msg.Creator, msg.Paused = signer.Address.String(), !e.params.ModerationPaused
		return e.submit(signer, msg, math.ZeroInt())
	})
}
