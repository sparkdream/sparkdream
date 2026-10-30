package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgUpvotePost delivers MsgUpvotePost from a random account on a
// visible post it did not write and has not reacted to. A non-member pays
// the reaction spam tax; the daily reaction cap is left to the dry-run.
func SimulateMsgUpvotePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUpvotePost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpvotePost{}
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
		if !e.member(voter) && e.params.ReactionSpamTaxAmount.IsPositive() {
			spent = e.params.ReactionSpamTaxAmount
		}
		return e.submit(voter, msg, spent)
	})
}

// reactionTarget picks a live account and a visible post it did not write
// and has not voted on. When there is no visible post, a random account
// first starts a thread (a real MsgCreatePost).
func (e *simEnv) reactionTarget() (simtypes.Account, types.Post, string, error) {
	voter, ok := pick(e.r, e.live())
	if !ok {
		return voter, types.Post{}, "no live signer", nil
	}
	addr := voter.Address.String()
	keep := func(p types.Post) bool { return visible(p) && p.Author != addr && !e.hasVoted(p.PostId, addr) }
	posts, err := e.postsWhere(keep)
	if err != nil {
		return voter, types.Post{}, "", err
	}
	if len(posts) == 0 {
		if _, _, err := e.ensureThread(notAddr(addr)); err != nil {
			return voter, types.Post{}, "", err
		}
		if posts, err = e.postsWhere(keep); err != nil {
			return voter, types.Post{}, "", err
		}
	}
	post, ok := pick(e.r, posts)
	if !ok {
		return voter, post, "no visible post the signer may vote on", nil
	}
	return voter, post, "", nil
}
