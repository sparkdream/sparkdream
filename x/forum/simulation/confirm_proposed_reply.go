package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgConfirmProposedReply delivers MsgConfirmProposedReply from the
// author of a thread with a pending sentinel proposal and no accepted reply
// (a sentinel proposal is first made, a real MsgMarkAcceptedReply, when
// none is pending). Proposals need a sentinel, so this op succeeds only in
// runs that bond one.
func SimulateMsgConfirmProposedReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgConfirmProposedReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgConfirmProposedReply{}
		roots, err := e.pendingProposals()
		if err != nil {
			return noOpErr(msg, err)
		}
		root, ok := pick(e.r, roots)
		if !ok {
			return noOp(msg, "no pending proposal on a sim account's thread")
		}
		author, _ := e.account(root.Author)
		msg.Creator, msg.ThreadId = root.Author, root.PostId
		return e.submit(author, msg, math.ZeroInt())
	})
}
