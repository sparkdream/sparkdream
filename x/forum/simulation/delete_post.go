package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgDeletePost delivers MsgDeletePost from the author of a visible
// post. Threads with an open bounty are left alone so the bounty ops keep
// targets.
func SimulateMsgDeletePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgDeletePost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDeletePost{}
		if e.params.ForumPaused {
			return noOp(msg, "forum is paused")
		}
		posts, err := e.postsWhere(func(p types.Post) bool {
			if _, own := e.account(p.Author); !own || !visible(p) {
				return false
			}
			_, bounty := e.openBounty(rootID(p))
			return !bounty
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		post, ok := pick(e.r, posts)
		if !ok {
			return noOp(msg, "no visible post by a sim account")
		}
		author, _ := e.account(post.Author)
		msg.Creator, msg.PostId = post.Author, post.PostId
		return e.submit(author, msg, math.ZeroInt())
	})
}
