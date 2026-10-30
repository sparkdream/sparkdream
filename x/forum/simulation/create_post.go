package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgCreatePost delivers MsgCreatePost from a random account: a new
// thread in a category the signer may write to (one in three), or a reply
// to a visible post (in a category the signer may write to) whose thread is neither hidden, archived nor locked and
// whose depth leaves room under max_reply_depth. Members post permanently;
// non-members post ephemerally and pay the spam tax on top of the storage
// fee.
func SimulateMsgCreatePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgCreatePost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		acc, _ := simtypes.RandomAcc(e.r, e.accs)
		opts := postOpts{}
		if e.r.Intn(3) > 0 {
			parents, err := e.postsWhere(func(p types.Post) bool { return e.replyable(p) && e.canPost(acc, &p) })
			if err != nil {
				return noOpErr(&types.MsgCreatePost{}, err)
			}
			if parent, ok := pick(e.r, parents); ok {
				opts.parent = &parent
			}
		}
		msg, spent, reason := e.newPostMsg(acc, opts)
		if reason != "" {
			return noOp(msg, reason)
		}
		return e.submit(acc, msg, spent)
	})
}
