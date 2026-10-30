package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgDisputePin delivers MsgDisputePin from the author of a thread
// whose reply a sentinel pinned, while that pin is not yet disputed. The
// author escrows x/rep's appeal bond. Pins only come from sentinels, so this
// op succeeds only in runs that bond one.
func SimulateMsgDisputePin(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgDisputePin{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDisputePin{}
		var targets []*types.MsgDisputePin
		err := e.k.ThreadMetadata.Walk(e.ctx, nil, func(threadID uint64, md types.ThreadMetadata) (bool, error) {
			root, ok := e.post(threadID)
			if !ok || root.ParentId != 0 {
				return false, nil
			}
			if _, ok := e.account(root.Author); !ok {
				return false, nil
			}
			for _, rec := range md.PinnedRecords {
				if rec != nil && rec.IsSentinelPin && !rec.Disputed {
					targets = append(targets, &types.MsgDisputePin{
						Creator: root.Author, ThreadId: threadID, ReplyId: rec.PostId,
						Reason: "aurora: the pinned reply is misleading",
					})
				}
			}
			return false, nil
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		t, ok := pick(e.r, targets)
		if !ok {
			return noOp(msg, "no undisputed sentinel pin in a sim account's thread")
		}
		author, _ := e.account(t.Creator)
		return e.submit(author, t, appealBond())
	})
}
