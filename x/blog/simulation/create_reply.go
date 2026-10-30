package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgCreateReply delivers MsgCreateReply from a random live account
// that passes the post's reply gate (min_reply_trust_level, or the post's
// own author): a top-level reply, or (one in three) a nested reply under an
// active reply that leaves room under max_reply_depth. With no replyable
// post, one is created first.
func SimulateMsgCreateReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgCreateReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgCreateReply{}
		post, ok := pick(e.r, e.postsWhere(e.replyable))
		if !ok {
			var err error
			post, ok, err = e.ensurePost(anyone)
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok || !e.replyable(post) {
				return noOp(label, "no replyable post and none could be created")
			}
		}
		acc, ok := pick(e.r, e.live(func(a simtypes.Account) bool { return e.canReply(a.Address.String(), post) }))
		if !ok {
			return noOp(label, "no live account passes the post's reply trust gate")
		}
		var parent *types.Reply
		if e.r.Intn(3) == 0 {
			nested := e.repliesWhere(func(rp types.Reply) bool { return rp.PostId == post.Id && e.nestable(rp) })
			if rp, ok := pick(e.r, nested); ok {
				parent = &rp
			}
		}
		msg, spent := e.newReplyMsg(acc, post, parent)
		return e.submit(acc, msg, spent)
	})
}
