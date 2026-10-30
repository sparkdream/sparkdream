package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgReact delivers MsgReact on a random active post (or, half the
// time, an active reply on it) from a random live account passing the
// post's reply gate. A new reaction pays the reaction fee; reacting again
// with another type changes the reaction for free. With no active post,
// one is created first.
func SimulateMsgReact(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgReact{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgReact{}
		post, ok := pick(e.r, e.postsWhere(activePost))
		if !ok {
			var err error
			post, ok, err = e.ensurePost(anyone)
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok {
				return noOp(label, "no active post and none could be created")
			}
		}
		acc, ok := pick(e.r, e.live(func(a simtypes.Account) bool { return e.canReply(a.Address.String(), post) }))
		if !ok {
			return noOp(label, "no live account passes the post's reaction trust gate")
		}
		msg := &types.MsgReact{
			Creator:      acc.Address.String(),
			PostId:       post.Id,
			ReactionType: randomReactionType(e.r),
		}
		if e.r.Intn(2) == 0 {
			onPost := e.repliesWhere(func(rp types.Reply) bool { return rp.PostId == post.Id && activeReply(rp) })
			if rp, ok := pick(e.r, onPost); ok {
				msg.ReplyId = rp.Id
			}
		}
		spent := zero()
		if _, reacted := e.k.GetReaction(e.ctx, msg.PostId, msg.ReplyId, msg.Creator); !reacted &&
			!e.params.ReactionFeeExempt && e.params.ReactionFeeAmount.IsPositive() {
			spent = e.params.ReactionFeeAmount
		}
		return e.submit(acc, msg, spent)
	})
}
