package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// SimulateMsgPinReply delivers MsgPinReply on the sentinel path: a
// CONTENT_SENTINEL holder (bonding one first through x/rep's MsgBondRole
// when no sim account holds the role) under the per-epoch pin cap and
// outside its overturn cooldown pins a permanent ACTIVE reply it did not
// write, in a thread with a free pinned-reply slot. The bond reservation is
// left to the dry-run. The council path (a Commons Operations Committee
// member) is not simulated here.
func SimulateMsgPinReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgPinReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgPinReply{}
		if e.params.ForumPaused || e.params.ModerationPaused {
			return noOp(msg, "forum or moderation paused")
		}
		sentinels, err := e.ensureSentinels()
		if err != nil {
			return noOpErr(msg, err)
		}
		sentinels = accountsWhere(sentinels, func(a simtypes.Account) bool {
			return e.sentinelActionsLeft(a.Address.String(), reptypes.ActionKindForumPin, types.MaxSentinelPinsPerEpoch)
		})
		sentinel, ok := pick(e.r, sentinels)
		if !ok {
			return noOp(msg, "no sentinel under its pin cap and outside cooldown")
		}
		addr := sentinel.Address.String()
		replies, err := e.postsWhere(func(p types.Post) bool {
			if p.ParentId == 0 || p.Status != types.PostStatus_POST_STATUS_ACTIVE || p.ExpirationTime > 0 || p.Author == addr {
				return false
			}
			md, _ := e.metadata(p.RootId)
			if uint64(len(md.PinnedReplyIds)) >= types.DefaultMaxPinnedRepliesPerThread {
				return false
			}
			for _, id := range md.PinnedReplyIds {
				if id == p.PostId {
					return false
				}
			}
			return true
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		reply, ok := pick(e.r, replies)
		if !ok {
			return noOp(msg, "no pinnable reply the sentinel did not write")
		}
		msg.Creator, msg.ThreadId, msg.ReplyId = addr, reply.RootId, reply.PostId
		return e.submit(sentinel, msg, math.ZeroInt())
	})
}
