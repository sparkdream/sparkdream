package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgDeleteReply delivers MsgDeleteReply for a random undeleted
// reply, signed by the reply's author or (one in three, when live) the
// post's author. With no such reply, one is created first.
func SimulateMsgDeleteReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgDeleteReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgDeleteReply{}
		deletable := func(rp types.Reply) bool {
			if rp.Status == types.ReplyStatus_REPLY_STATUS_DELETED {
				return false
			}
			_, ok := e.deleter(rp)
			return ok
		}
		rp, ok, err := e.replyOrCreate(deletable, anyone)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no undeleted reply with a live author and none could be created")
		}
		acc, _ := e.deleter(rp)
		return e.submit(acc, &types.MsgDeleteReply{Creator: acc.Address.String(), Id: rp.Id}, zero())
	})
}

// deleter returns a live sim account allowed to delete rp: the post's
// author one time in three (when live), else the reply's author.
func (e *simEnv) deleter(rp types.Reply) (simtypes.Account, bool) {
	replyAuthor, replyOK := e.owner(rp.Creator)
	var postAuthor simtypes.Account
	postOK := false
	if post, found := e.k.GetPost(e.ctx, rp.PostId); found {
		postAuthor, postOK = e.owner(post.Creator)
	}
	switch {
	case postOK && (!replyOK || e.r.Intn(3) == 0):
		return postAuthor, true
	case replyOK:
		return replyAuthor, true
	}
	return simtypes.Account{}, false
}
