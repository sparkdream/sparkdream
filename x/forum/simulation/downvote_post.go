package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgDownvotePost delivers MsgDownvotePost from a random account on
// a visible post it did not write and has not reacted to, burning the
// downvote deposit; the daily downvote cap is left to the dry-run.
func SimulateMsgDownvotePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgDownvotePost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDownvotePost{}
		if e.params.ForumPaused || !e.params.ReactionsEnabled {
			return noOp(msg, "forum paused or reactions disabled")
		}
		voter, post, reason, err := e.reactionTarget()
		if err != nil {
			return noOpErr(msg, err)
		}
		if reason != "" {
			return noOp(msg, reason)
		}
		msg.Creator, msg.PostId = voter.Address.String(), post.PostId
		spent := math.ZeroInt()
		if e.params.DownvoteDepositAmount.IsPositive() {
			spent = e.params.DownvoteDepositAmount
		}
		return e.submit(voter, msg, spent)
	})
}
