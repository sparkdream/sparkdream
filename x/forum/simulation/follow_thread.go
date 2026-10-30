package simulation

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgFollowThread delivers MsgFollowThread from a random account on
// a visible thread it does not follow yet (the daily follow cap is left to
// the dry-run).
func SimulateMsgFollowThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgFollowThread{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgFollowThread{}
		if e.params.ForumPaused {
			return noOp(msg, "forum is paused")
		}
		acc, ok := pick(e.r, e.live())
		if !ok {
			return noOp(msg, "no live signer")
		}
		threads, err := e.followable(acc.Address.String())
		if err != nil {
			return noOpErr(msg, err)
		}
		thread, ok := pick(e.r, threads)
		if !ok {
			return noOp(msg, "no visible thread the signer does not follow")
		}
		msg.Creator, msg.ThreadId = acc.Address.String(), thread.PostId
		return e.submit(acc, msg, math.ZeroInt())
	})
}

// followable returns the visible threads addr does not follow.
func (e *simEnv) followable(addr string) ([]types.Post, error) {
	return e.postsWhere(func(p types.Post) bool {
		if p.ParentId != 0 || !visible(p) {
			return false
		}
		has, err := e.k.ThreadFollow.Has(e.ctx, fmt.Sprintf("%s:%d", addr, p.PostId))
		return err == nil && !has
	})
}
