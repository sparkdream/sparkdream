package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgUnlockThread delivers MsgUnlockThread from the sentinel that
// locked a thread, while its lock record is not under appeal and the lock
// appeal deadline has not passed (or from a Commons Operations Committee
// member, when a sim account is one, for any lock not under appeal).
func SimulateMsgUnlockThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnlockThread{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUnlockThread{}
		council := e.opsCommittee()
		type unlock struct {
			signer simtypes.Account
			root   uint64
		}
		var targets []unlock
		roots, err := e.postsWhere(func(p types.Post) bool { return p.ParentId == 0 && p.Locked })
		if err != nil {
			return noOpErr(msg, err)
		}
		for _, root := range roots {
			lr, err := e.k.ThreadLockRecord.Get(e.ctx, root.PostId)
			if err == nil && lr.AppealPending {
				continue
			}
			for _, c := range council {
				targets = append(targets, unlock{c, root.PostId})
			}
			if err != nil || lr.Sentinel == "" || lr.Sentinel != root.LockedBy ||
				e.now() >= lr.LockedAt+types.DefaultLockAppealDeadline {
				continue
			}
			if acc, ok := e.account(lr.Sentinel); ok {
				targets = append(targets, unlock{acc, root.PostId})
			}
		}
		t, ok := pick(e.r, targets)
		if !ok {
			return noOp(msg, "no lock the signer may lift")
		}
		msg.Creator, msg.RootId = t.signer.Address.String(), t.root
		return e.submit(t.signer, msg, math.ZeroInt())
	})
}
