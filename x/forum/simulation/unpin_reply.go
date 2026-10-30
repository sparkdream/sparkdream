package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgUnpinReply delivers MsgUnpinReply from the sentinel that pinned
// a reply (or a Commons Operations Committee member, when a sim account is
// one), for a pin that is not under dispute.
func SimulateMsgUnpinReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnpinReply{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUnpinReply{}
		council := e.opsCommittee()
		var targets []*types.MsgUnpinReply
		signers := map[string]simtypes.Account{}
		err := e.k.ThreadMetadata.Walk(e.ctx, nil, func(threadID uint64, md types.ThreadMetadata) (bool, error) {
			for _, rec := range md.PinnedRecords {
				if rec == nil || (rec.Disputed && rec.CommittedAmount != "") {
					continue
				}
				cands := council
				if acc, ok := e.account(rec.PinnedBy); ok {
					cands = append(append([]simtypes.Account{}, council...), acc)
				}
				for _, c := range cands {
					signers[c.Address.String()] = c
					targets = append(targets, &types.MsgUnpinReply{Creator: c.Address.String(), ThreadId: threadID, ReplyId: rec.PostId})
				}
			}
			return false, nil
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		t, ok := pick(e.r, targets)
		if !ok {
			return noOp(msg, "no undisputed pin the signer may remove")
		}
		return e.submit(signers[t.Creator], t, math.ZeroInt())
	})
}
