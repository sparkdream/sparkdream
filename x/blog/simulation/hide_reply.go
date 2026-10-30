package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgHideReply delivers MsgHideReply for a random active reply,
// signed by the post's author (by alias): only the post author may hide
// replies on it. With no such reply, one is created first.
func SimulateMsgHideReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgHideReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgHideReply{}
		hideable := func(rp types.Reply) bool {
			_, ok := e.postOwner(rp)
			return activeReply(rp) && ok
		}
		rp, ok, err := e.replyOrCreate(hideable, anyone)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no active reply on a post with a live author and none could be created")
		}
		acc, _ := e.postOwner(rp)
		return e.submit(acc, &types.MsgHideReply{Creator: acc.Address.String(), Id: rp.Id}, zero())
	})
}

// postOwner returns the live sim account that may act for the author of
// rp's post.
func (e *simEnv) postOwner(rp types.Reply) (simtypes.Account, bool) {
	post, found := e.k.GetPost(e.ctx, rp.PostId)
	if !found {
		return simtypes.Account{}, false
	}
	return e.owner(post.Creator)
}
