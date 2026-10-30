package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commonstypes "sparkdream/x/commons/types"
	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// SimulateMsgMoveThread delivers MsgMoveThread on the sentinel path: a
// CONTENT_SENTINEL holder (bonding one first through x/rep's MsgBondRole
// when no sim account holds the role) under max_sentinel_moves_per_epoch
// and outside its overturn cooldown moves a visible thread it did not
// write, with no open move record, into another existing category. The
// reserved-tag gate and the bond reservation are left to the dry-run. The
// council path (a Commons Operations Committee member) is not simulated
// here.
func SimulateMsgMoveThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgMoveThread{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgMoveThread{}
		if e.params.ForumPaused || e.params.ModerationPaused {
			return noOp(msg, "forum or moderation paused")
		}
		if len(e.liveCategories()) < 2 {
			return noOp(msg, "fewer than two categories")
		}
		sentinels, err := e.ensureSentinels()
		if err != nil {
			return noOpErr(msg, err)
		}
		sentinels = accountsWhere(sentinels, func(a simtypes.Account) bool {
			return e.sentinelActionsLeft(a.Address.String(), reptypes.ActionKindForumMove, e.params.MaxSentinelMovesPerEpoch)
		})
		sentinel, ok := pick(e.r, sentinels)
		if !ok {
			return noOp(msg, "no sentinel under its move cap and outside cooldown")
		}
		addr := sentinel.Address.String()
		roots, err := e.postsWhere(func(p types.Post) bool {
			if p.ParentId != 0 || !visible(p) || p.Author == addr {
				return false
			}
			has, err := e.k.ThreadMoveRecord.Has(e.ctx, p.PostId)
			return err == nil && !has
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		root, ok := pick(e.r, roots)
		if !ok {
			return noOp(msg, "no movable thread the sentinel did not write")
		}
		var others []commonstypes.Category
		for _, c := range e.liveCategories() {
			if c.CategoryId != root.CategoryId {
				others = append(others, c)
			}
		}
		dest, ok := pick(e.r, others)
		if !ok {
			return noOp(msg, "no other category")
		}
		msg.Creator, msg.RootId, msg.NewCategoryId = addr, root.PostId, dest.CategoryId
		msg.Reason = "zenith: belongs in another category"
		msg.Authority = types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL
		return e.submit(sentinel, msg, math.ZeroInt())
	})
}
