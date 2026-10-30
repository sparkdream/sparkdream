package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgFreezeThread delivers MsgFreezeThread (archive) from a random
// account on an ACTIVE thread without an open bounty, inactive for
// archive_threshold, with no unresolved hide on any of its posts, no pending
// lock or move appeal, outside the archive cooldown and under the archive
// cycle cap. With the default 30-day archive_threshold no thread ages enough
// inside a 200-block run (about 17 days of block time), so this op is a
// NoOp in the standard full-app simulation.
func SimulateMsgFreezeThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgFreezeThread{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgFreezeThread{}
		if e.params.ForumPaused {
			return noOp(msg, "forum is paused")
		}
		threshold := e.params.ArchiveThreshold
		if threshold == 0 {
			threshold = types.DefaultArchiveThreshold
		}
		// Cheap filter first: a thread whose root is younger than the
		// threshold cannot be inactive for it.
		roots, err := e.postsWhere(func(p types.Post) bool {
			return activeRoot(p) && e.now()-p.CreatedAt >= threshold
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		var candidates []types.Post
		for _, root := range roots {
			ok, err := e.archivable(root, threshold)
			if err != nil {
				return noOpErr(msg, err)
			}
			if ok {
				candidates = append(candidates, root)
			}
		}
		root, ok := pick(e.r, candidates)
		if !ok {
			return noOp(msg, "no thread inactive for archive_threshold")
		}
		acc, ok := pick(e.r, e.live())
		if !ok {
			return noOp(msg, "no live signer")
		}
		msg.Creator, msg.RootId = acc.Address.String(), root.PostId
		return e.submit(acc, msg, math.ZeroInt())
	})
}

// archivable mirrors FreezeThread's thread gates for an ACTIVE root.
func (e *simEnv) archivable(root types.Post, threshold int64) (bool, error) {
	if _, bounty := e.openBounty(root.PostId); bounty {
		return false, nil
	}
	if has, err := e.k.HideRecord.Has(e.ctx, root.PostId); err != nil || has {
		return false, err
	}
	if lr, err := e.k.ThreadLockRecord.Get(e.ctx, root.PostId); err == nil && lr.AppealPending {
		return false, nil
	}
	if mr, err := e.k.ThreadMoveRecord.Get(e.ctx, root.PostId); err == nil && mr.AppealPending {
		return false, nil
	}
	if am, err := e.k.ArchiveMetadata.Get(e.ctx, root.PostId); err == nil {
		cooldown := e.params.ArchiveCooldown
		if cooldown == 0 {
			cooldown = types.DefaultArchiveCooldown
		}
		if am.LastUnarchivedAt > 0 && e.now() < am.LastUnarchivedAt+cooldown {
			return false, nil
		}
		if am.ArchiveCount >= types.DefaultMaxArchiveCycles {
			return false, nil
		}
	}
	replies, err := e.threadReplies(root.PostId)
	if err != nil {
		return false, err
	}
	if uint64(len(replies))+1 > types.MaxArchivePostCount {
		return false, nil
	}
	last := max(root.CreatedAt, root.EditedAt)
	for _, r := range replies {
		if has, err := e.k.HideRecord.Has(e.ctx, r.PostId); err != nil || has {
			return false, err
		}
		last = max(last, r.CreatedAt, r.EditedAt)
	}
	return e.now()-last >= threshold, nil
}
