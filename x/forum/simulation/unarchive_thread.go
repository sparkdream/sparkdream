package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgUnarchiveThread delivers MsgUnarchiveThread from a random
// account on an archived thread whose category still exists, once
// unarchive_cooldown has passed since it was archived. Threads are only
// archived by MsgFreezeThread, which cannot succeed in a 200-block run (see
// SimulateMsgFreezeThread), so this op is a NoOp there.
func SimulateMsgUnarchiveThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnarchiveThread{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUnarchiveThread{}
		if e.params.ForumPaused {
			return noOp(msg, "forum is paused")
		}
		cooldown := e.params.UnarchiveCooldown
		if cooldown == 0 {
			cooldown = types.DefaultUnarchiveCooldown
		}
		roots, err := e.postsWhere(func(p types.Post) bool {
			if p.ParentId != 0 || p.Status != types.PostStatus_POST_STATUS_ARCHIVED {
				return false
			}
			if _, ok := e.category(p.CategoryId); !ok {
				return false
			}
			am, err := e.k.ArchiveMetadata.Get(e.ctx, p.PostId)
			return err != nil || e.now()-am.LastArchivedAt >= cooldown
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		root, ok := pick(e.r, roots)
		if !ok {
			return noOp(msg, "no archived thread past unarchive_cooldown")
		}
		acc, ok := pick(e.r, e.live())
		if !ok {
			return noOp(msg, "no live signer")
		}
		msg.Creator, msg.RootId = acc.Address.String(), root.PostId
		return e.submit(acc, msg, math.ZeroInt())
	})
}
