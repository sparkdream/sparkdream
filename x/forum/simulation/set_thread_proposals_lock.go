package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgSetThreadProposalsLock delivers MsgSetThreadProposalsLock from
// the author of a thread, flipping whether sentinels may propose an
// accepted reply on it (locking also clears a pending proposal).
func SimulateMsgSetThreadProposalsLock(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgSetThreadProposalsLock{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSetThreadProposalsLock{}
		roots, err := e.postsWhere(func(p types.Post) bool {
			_, own := e.account(p.Author)
			return own && p.ParentId == 0 && visible(p)
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		root, ok := pick(e.r, roots)
		if !ok {
			return noOp(msg, "no visible thread by a sim account")
		}
		md, _ := e.metadata(root.PostId)
		author, _ := e.account(root.Author)
		msg.Creator, msg.ThreadId, msg.Locked = root.Author, root.PostId, !md.ProposalsLocked
		return e.submit(author, msg, math.ZeroInt())
	})
}
