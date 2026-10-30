package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgAppealThreadMove delivers MsgAppealThreadMove from the author
// of a thread a sentinel moved, after move_appeal_cooldown and before the
// move appeal deadline, while no appeal is pending. The appellant escrows
// x/rep's appeal bond. Moves only come from sentinels, so this op succeeds
// only in runs that bond one.
func SimulateMsgAppealThreadMove(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgAppealThreadMove{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAppealThreadMove{}
		if e.params.AppealsPaused {
			return noOp(msg, "appeals are paused")
		}
		var roots []types.Post
		err := e.k.ThreadMoveRecord.Walk(e.ctx, nil, func(id uint64, mr types.ThreadMoveRecord) (bool, error) {
			// Only sentinel moves are appealed here: a council move goes
			// through x/rep's governance-action appeal.
			if mr.Sentinel == "" || mr.AppealPending || e.now() < mr.MovedAt+e.params.MoveAppealCooldown ||
				e.now() >= mr.MovedAt+types.DefaultMoveAppealDeadline {
				return false, nil
			}
			p, ok := e.post(id)
			if !ok {
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
			return noOp(msg, "no appealable sentinel move on a sim account's thread")
		}
		author, _ := e.account(root.Author)
		msg.Creator, msg.RootId = root.Author, root.PostId
		return e.submit(author, msg, appealBond())
	})
}
