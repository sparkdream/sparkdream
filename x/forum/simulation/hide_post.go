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

// SimulateMsgHidePost delivers MsgHidePost on the sentinel path: a
// CONTENT_SENTINEL bonded-role holder (bonding one first through x/rep's
// MsgBondRole when no sim account holds the role) under max_hides_per_epoch
// and outside its overturn cooldown hides a visible post it did not write;
// the bond reservation is left to the dry-run. The council path (a Commons
// Operations Committee member) is not simulated here.
func SimulateMsgHidePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgHidePost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgHidePost{}
		if e.params.ModerationPaused {
			return noOp(msg, "moderation is paused")
		}
		sentinels, err := e.ensureSentinels()
		if err != nil {
			return noOpErr(msg, err)
		}
		sentinels = accountsWhere(sentinels, func(a simtypes.Account) bool {
			return e.sentinelActionsLeft(a.Address.String(), reptypes.ActionKindForumHide, e.params.MaxHidesPerEpoch)
		})
		sentinel, ok := pick(e.r, sentinels)
		if !ok {
			return noOp(msg, "no sentinel under its hide cap and outside cooldown")
		}
		addr := sentinel.Address.String()
		posts, err := e.postsWhere(func(p types.Post) bool { return visible(p) && p.Author != addr })
		if err != nil {
			return noOpErr(msg, err)
		}
		post, ok := pick(e.r, posts)
		if !ok {
			return noOp(msg, "no visible post the sentinel did not write")
		}
		reason := randomModerationReason(e.r)
		msg.Creator, msg.PostId = addr, post.PostId
		msg.ReasonCode = commontypes.Saturate[uint64](int32(reason))
		if reason == commontypes.ModerationReason_MODERATION_REASON_OTHER {
			msg.ReasonText = randomReason(e.r)
		}
		msg.Authority = types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL
		if e.r.Intn(2) == 0 {
			msg.Authority = types.ModerationAuthority_MODERATION_AUTHORITY_AUTO
		}
		return e.submit(sentinel, msg, math.ZeroInt())
	})
}
