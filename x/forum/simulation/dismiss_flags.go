package simulation

import (
	"strconv"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgDismissFlags delivers MsgDismissFlags from an eligible
// CONTENT_SENTINEL (bonding one first through x/rep's MsgBondRole when no
// sim account holds the role) on a post in the flag review queue that the
// sentinel did not write. When no post is in the queue, other accounts
// first flag one (real MsgFlagPost txs) until its weight reaches the review
// threshold. The council path needs a Commons Operations Committee member,
// which the op uses when one exists among the sim accounts.
func SimulateMsgDismissFlags(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgDismissFlags{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDismissFlags{}
		if e.params.ForumPaused || e.params.ModerationPaused {
			return noOp(msg, "forum or moderation paused")
		}
		sentinels, err := e.ensureSentinels()
		if err != nil {
			return noOpErr(msg, err)
		}
		sentinel, ok := pick(e.r, sentinels)
		if !ok {
			return noOp(msg, "no sim account holds or can bond CONTENT_SENTINEL")
		}
		addr := sentinel.Address.String()
		queued, err := e.reviewQueue(addr)
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(queued) == 0 {
			if err := e.fillReviewQueue(addr); err != nil {
				return noOpErr(msg, err)
			}
			if queued, err = e.reviewQueue(addr); err != nil {
				return noOpErr(msg, err)
			}
		}
		postID, ok := pick(e.r, queued)
		if !ok {
			return noOp(msg, "no post in the flag review queue the sentinel did not write")
		}
		msg.Creator, msg.PostId, msg.Reason = addr, postID, "phoenix review: flags not upheld"
		return e.submit(sentinel, msg, math.ZeroInt())
	})
}

// reviewQueue returns the ids of posts in the flag review queue that
// sentinel did not write.
func (e *simEnv) reviewQueue(sentinel string) ([]uint64, error) {
	var out []uint64
	err := e.k.PostFlag.Walk(e.ctx, nil, func(id uint64, pf types.PostFlag) (bool, error) {
		if !pf.InReviewQueue {
			return false, nil
		}
		if p, ok := e.post(id); ok && p.Author != sentinel {
			out = append(out, id)
		}
		return false, nil
	})
	return out, err
}

// fillReviewQueue flags one visible post not written by exclude from
// distinct accounts until its weight reaches the review threshold.
func (e *simEnv) fillReviewQueue(exclude string) error {
	posts, err := e.postsWhere(func(p types.Post) bool { return visible(p) && p.Author != exclude })
	if err != nil {
		return err
	}
	post, ok := pick(e.r, posts)
	if !ok {
		return nil
	}
	for _, i := range e.r.Perm(len(e.accs)) {
		pf, err := e.k.PostFlag.Get(e.ctx, post.PostId)
		if err == nil {
			if pf.InReviewQueue {
				return nil
			}
			if w, perr := strconv.ParseUint(pf.TotalWeight, 10, 64); perr == nil && w >= types.DefaultFlagReviewThreshold {
				return nil
			}
		}
		flagger := e.accs[i]
		addr := flagger.Address.String()
		if addr == post.Author || addr == exclude {
			continue
		}
		msg := e.newFlagMsg(flagger, post.PostId)
		if _, err := e.ensure(flagger, msg, e.flagCost(flagger)); err != nil {
			return err
		}
	}
	return nil
}
