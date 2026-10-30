package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgEditPost delivers MsgEditPost from the author of a visible post
// still inside edit_max_window (creating a thread first when no sim account
// has one). The new content differs from the old; the signer pays the
// storage fee on any growth and, past edit_grace_period, the edit fee.
func SimulateMsgEditPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgEditPost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgEditPost{}
		p := e.params
		if p.ForumPaused || !p.EditingEnabled {
			return noOp(msg, "forum paused or editing disabled")
		}
		window := p.EditMaxWindow
		if window == 0 {
			window = types.DefaultEditMaxWindow
		}
		editable := func(post types.Post) bool {
			_, own := e.account(post.Author)
			return own && visible(post) && e.now()-post.CreatedAt <= window
		}
		posts, err := e.postsWhere(editable)
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(posts) == 0 {
			if _, _, err := e.ensureThread(anyone); err != nil {
				return noOpErr(msg, err)
			}
			if posts, err = e.postsWhere(editable); err != nil {
				return noOpErr(msg, err)
			}
		}
		post, ok := pick(e.r, posts)
		if !ok {
			return noOp(msg, "no editable post by a sim account")
		}
		author, _ := e.account(post.Author)
		msg.Creator, msg.PostId = post.Author, post.PostId
		msg.ContentType = commontypes.ContentType_CONTENT_TYPE_TEXT
		msg.NewContent = randomContent(e.r)
		if msg.NewContent == post.Content {
			msg.NewContent += " (edited)"
		}
		msg.Tags = post.Tags
		if e.r.Intn(3) == 0 {
			msg.Tags = e.randomTags()
		}

		spent := math.ZeroInt()
		if !p.CostPerByteExempt && p.CostPerByteAmount.IsPositive() && len(msg.NewContent) > len(post.Content) {
			spent = spent.Add(p.CostPerByteAmount.MulRaw(int64(len(msg.NewContent) - len(post.Content))))
		}
		if e.now()-post.CreatedAt > p.EditGracePeriod && p.EditFeeAmount.IsPositive() {
			spent = spent.Add(p.EditFeeAmount)
		}
		return e.submit(author, msg, spent)
	})
}
