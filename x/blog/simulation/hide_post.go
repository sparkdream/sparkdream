package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgHidePost delivers MsgHidePost from the author (by alias) of a
// random active post; blog hiding is author-only. With no such post, one is
// created first.
func SimulateMsgHidePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgHidePost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgHidePost{}
		post, acc, ok, err := e.ownedPostOrCreate(activePost, anyone)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no active post with a live author and none could be created")
		}
		return e.submit(acc, &types.MsgHidePost{Creator: acc.Address.String(), Id: post.Id}, zero())
	})
}
