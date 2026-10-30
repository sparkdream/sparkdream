package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// SimulateMsgMakePostPermanent delivers MsgMakePostPermanent from an active
// member at make_permanent_min_trust_level on an unexpired ephemeral post
// that is neither hidden nor deleted. When no such post exists, a
// non-member first writes one (a real MsgCreatePost, paying the spam tax).
// The per-day promotion cap is left to the dry-run.
func SimulateMsgMakePostPermanent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgMakePostPermanent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgMakePostPermanent{}
		if e.params.ForumPaused {
			return noOp(msg, "forum is paused")
		}
		level := reptypes.TrustLevel(commontypes.Saturate[int32](e.params.MakePermanentMinTrustLevel))
		promoters := accountsWhere(e.live(), func(a simtypes.Account) bool { return e.trustAtLeast(a.Address.String(), level) })
		promoter, ok := pick(e.r, promoters)
		if !ok {
			return noOp(msg, "no member at make_permanent_min_trust_level")
		}
		ephemeral := func(p types.Post) bool {
			return p.ExpirationTime > e.now() &&
				p.Status != types.PostStatus_POST_STATUS_HIDDEN && p.Status != types.PostStatus_POST_STATUS_DELETED
		}
		posts, err := e.postsWhere(ephemeral)
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(posts) == 0 {
			if _, _, err := e.ensureThread(func(a simtypes.Account) bool { return !e.member(a) }); err != nil {
				return noOpErr(msg, err)
			}
			if posts, err = e.postsWhere(ephemeral); err != nil {
				return noOpErr(msg, err)
			}
		}
		post, ok := pick(e.r, posts)
		if !ok {
			return noOp(msg, "no unexpired ephemeral post")
		}
		msg.Creator, msg.PostId = promoter.Address.String(), post.PostId
		return e.submit(promoter, msg, math.ZeroInt())
	})
}
