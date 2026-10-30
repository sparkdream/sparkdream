package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgDeletePost delivers MsgDeletePost from the author (by alias) of
// a random active or hidden post; the handler tombstones it. With no such
// post, one is created first (and then deleted).
func SimulateMsgDeletePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgDeletePost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgDeletePost{}
		notDeleted := func(p types.Post) bool { return p.Status != types.PostStatus_POST_STATUS_DELETED }
		post, acc, ok, err := e.ownedPostOrCreate(notDeleted, anyone)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no undeleted post with a live author and none could be created")
		}
		return e.submit(acc, &types.MsgDeletePost{Creator: acc.Address.String(), Id: post.Id}, zero())
	})
}
