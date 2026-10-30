package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgUnfollowThread delivers MsgUnfollowThread for a follow held by
// a sim account. When no sim account follows anything, one first follows a
// visible thread (a real MsgFollowThread).
func SimulateMsgUnfollowThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnfollowThread{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUnfollowThread{}
		follows, err := e.simFollows()
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(follows) == 0 && !e.params.ForumPaused {
			if acc, ok := pick(e.r, e.live()); ok {
				threads, err := e.followable(acc.Address.String())
				if err != nil {
					return noOpErr(msg, err)
				}
				if thread, ok := pick(e.r, threads); ok {
					follow := &types.MsgFollowThread{Creator: acc.Address.String(), ThreadId: thread.PostId}
					if _, err := e.ensure(acc, follow, math.ZeroInt()); err != nil {
						return noOpErr(msg, err)
					}
				}
			}
			if follows, err = e.simFollows(); err != nil {
				return noOpErr(msg, err)
			}
		}
		f, ok := pick(e.r, follows)
		if !ok {
			return noOp(msg, "no sim account follows a thread")
		}
		acc, _ := e.account(f.Follower)
		msg.Creator, msg.ThreadId = f.Follower, f.ThreadId
		return e.submit(acc, msg, math.ZeroInt())
	})
}

// simFollows returns the follows held by live sim accounts.
func (e *simEnv) simFollows() ([]types.ThreadFollow, error) {
	var out []types.ThreadFollow
	err := e.k.ThreadFollow.Walk(e.ctx, nil, func(_ string, f types.ThreadFollow) (bool, error) {
		if _, ok := e.account(f.Follower); ok {
			out = append(out, f)
		}
		return false, nil
	})
	return out, err
}
