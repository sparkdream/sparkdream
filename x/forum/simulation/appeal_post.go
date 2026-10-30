package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgAppealPost delivers MsgAppealPost from the author of a post a
// sentinel hid, once hide_appeal_cooldown has passed and while the hide is
// not yet appealed. The appellant escrows x/rep's appeal bond. Hides only
// come from sentinels, so this op succeeds only in runs that bond one.
func SimulateMsgAppealPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgAppealPost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAppealPost{}
		if e.params.AppealsPaused {
			return noOp(msg, "appeals are paused")
		}
		var posts []types.Post
		err := e.k.HideRecord.Walk(e.ctx, nil, func(id uint64, hr types.HideRecord) (bool, error) {
			if hr.Sentinel == "" || hr.Appealed || e.now() < hr.HiddenAt+e.params.HideAppealCooldown {
				return false, nil
			}
			p, ok := e.post(id)
			if !ok || p.Status != types.PostStatus_POST_STATUS_HIDDEN {
				return false, nil
			}
			if _, ok := e.account(p.Author); ok {
				posts = append(posts, p)
			}
			return false, nil
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		post, ok := pick(e.r, posts)
		if !ok {
			return noOp(msg, "no appealable sentinel hide on a sim account's post")
		}
		author, _ := e.account(post.Author)
		msg.Creator, msg.PostId = post.Author, post.PostId
		return e.submit(author, msg, appealBond())
	})
}
