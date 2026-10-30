package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgDownvoteContent delivers MsgDownvoteContent under the same
// target and voter rules as upvotes (max_downvotes_per_day); the voter burns
// downvote_cost SPARK.
func SimulateMsgDownvoteContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgDownvoteContent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDownvoteContent{}
		voter, t, reason, err := e.reactionTarget("downvote", e.params.MaxDownvotesPerDay)
		if err != nil {
			return noOpErr(msg, err)
		}
		if reason != "" {
			return noOp(msg, reason)
		}
		msg.Creator, msg.TargetId, msg.TargetType = voter.Address.String(), t.id, t.typ
		return e.submit(voter, msg, e.params.DownvoteCost)
	})
}
