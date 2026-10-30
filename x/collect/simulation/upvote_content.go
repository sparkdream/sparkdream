package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgUpvoteContent delivers MsgUpvoteContent from an active member
// on a public, active, feedback-enabled collection or non-hidden item that
// is not its own or one it collaborates on, has not reacted to yet, under
// max_upvotes_per_day.
func SimulateMsgUpvoteContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUpvoteContent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpvoteContent{}
		voter, t, reason, err := e.reactionTarget("upvote", e.params.MaxUpvotesPerDay)
		if err != nil {
			return noOpErr(msg, err)
		}
		if reason != "" {
			return noOp(msg, reason)
		}
		msg.Creator, msg.TargetId, msg.TargetType = voter.Address.String(), t.id, t.typ
		return e.submit(voter, msg, math.ZeroInt())
	})
}
