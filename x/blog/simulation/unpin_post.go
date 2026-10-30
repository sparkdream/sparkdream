package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgUnpinPost delivers MsgUnpinPost on a random active pinned post
// from a live member at pin_min_trust_level (any such member may unpin).
// With no pinned post, a member pins one first (a real MsgPinPost).
func SimulateMsgUnpinPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnpinPost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgUnpinPost{}
		pinner, ok := e.pinner()
		if !ok {
			return noOp(label, "no live member at pin_min_trust_level")
		}
		post, ok := pick(e.r, e.postsWhere(func(p types.Post) bool { return activePost(p) && p.PinnedBy != "" }))
		if !ok {
			post, ok = pick(e.r, e.postsWhere(func(p types.Post) bool { return activePost(p) && p.ExpiresAt == 0 && p.PinnedBy == "" }))
			if !ok {
				return noOp(label, "no pinned post and no permanent post to pin")
			}
			pinned, err := e.ensure(pinner, &types.MsgPinPost{Creator: pinner.Address.String(), Id: post.Id}, zero())
			if err != nil {
				return noOpErr(label, err)
			}
			if !pinned {
				return noOp(label, "no pinned post and none could be pinned")
			}
		}
		return e.submit(pinner, &types.MsgUnpinPost{Creator: pinner.Address.String(), Id: post.Id}, zero())
	})
}
