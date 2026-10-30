package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgAppealThreadLock delivers MsgAppealThreadLock from the author
// of a thread a sentinel locked, after lock_appeal_cooldown and before the
// lock appeal deadline, while no appeal is pending. The appellant escrows
// x/rep's appeal bond. Locks need a well-bonded sentinel, so this op rarely
// has a target.
func SimulateMsgAppealThreadLock(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgAppealThreadLock{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAppealThreadLock{}
		if e.params.AppealsPaused {
			return noOp(msg, "appeals are paused")
		}
		var roots []types.Post
		err := e.k.ThreadLockRecord.Walk(e.ctx, nil, func(id uint64, lr types.ThreadLockRecord) (bool, error) {
			// Only sentinel locks are appealed here: a council lock goes
			// through x/rep's governance-action appeal.
			if lr.Sentinel == "" || lr.AppealPending || e.now() < lr.LockedAt+e.params.LockAppealCooldown ||
				e.now() >= lr.LockedAt+types.DefaultLockAppealDeadline {
				return false, nil
			}
			p, ok := e.post(id)
			if !ok || !p.Locked {
				return false, nil
			}
			if _, ok := e.account(p.Author); ok {
				roots = append(roots, p)
			}
			return false, nil
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		root, ok := pick(e.r, roots)
		if !ok {
			return noOp(msg, "no appealable sentinel lock on a sim account's thread")
		}
		author, _ := e.account(root.Author)
		msg.Creator, msg.RootId = root.Author, root.PostId
		return e.submit(author, msg, appealBond())
	})
}
