package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgUnhidePost delivers MsgUnhidePost from the author (by alias) of
// a random hidden post. With none, the author of an active post hides it
// first (a real MsgHidePost) and then unhides it.
func SimulateMsgUnhidePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnhidePost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgUnhidePost{}
		hidden := func(p types.Post) bool { return p.Status == types.PostStatus_POST_STATUS_HIDDEN }
		post, acc, ok, err := e.ownedPostOrCreate(hidden, func(simtypes.Account) bool { return false })
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			post, acc, ok, err = e.ownedPostOrCreate(activePost, anyone)
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok {
				return noOp(label, "no hidden or active post with a live author")
			}
			hid, err := e.ensure(acc, &types.MsgHidePost{Creator: acc.Address.String(), Id: post.Id}, zero())
			if err != nil {
				return noOpErr(label, err)
			}
			if !hid {
				return noOp(label, "no hidden post and none could be hidden")
			}
		}
		return e.submit(acc, &types.MsgUnhidePost{Creator: acc.Address.String(), Id: post.Id}, zero())
	})
}
