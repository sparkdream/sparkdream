package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgRemoveReaction delivers MsgRemoveReaction for a reaction a
// random live account holds (read through the ListReactionsByCreator
// query, which follows the alias contract). With none, the account reacts
// first (a real MsgReact) and then removes it.
func SimulateMsgRemoveReaction(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgRemoveReaction{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgRemoveReaction{}
		accs := e.live(anyone)
		for _, i := range e.r.Perm(len(accs)) {
			acc := accs[i]
			res, err := e.blogQuery.ListReactionsByCreator(e.ctx, &types.QueryListReactionsByCreatorRequest{Creator: acc.Address.String()})
			if err != nil {
				return noOpErr(label, err)
			}
			if rx, ok := pick(e.r, res.Reactions); ok {
				return e.submit(acc, &types.MsgRemoveReaction{Creator: acc.Address.String(), PostId: rx.PostId, ReplyId: rx.ReplyId}, zero())
			}
		}
		// Nobody holds a reaction: react first, then remove it.
		post, ok := pick(e.r, e.postsWhere(activePost))
		if !ok {
			return noOp(label, "no reaction and no active post to react to")
		}
		acc, ok := pick(e.r, e.live(func(a simtypes.Account) bool { return e.canReply(a.Address.String(), post) }))
		if !ok {
			return noOp(label, "no reaction and no live account passes the post's reaction gate")
		}
		react := &types.MsgReact{Creator: acc.Address.String(), PostId: post.Id, ReactionType: randomReactionType(e.r)}
		fee := zero()
		if !e.params.ReactionFeeExempt && e.params.ReactionFeeAmount.IsPositive() {
			fee = e.params.ReactionFeeAmount
		}
		reacted, err := e.ensure(acc, react, fee)
		if err != nil {
			return noOpErr(label, err)
		}
		if !reacted {
			return noOp(label, "no reaction and none could be added")
		}
		return e.submit(acc, &types.MsgRemoveReaction{Creator: acc.Address.String(), PostId: post.Id}, zero())
	})
}
