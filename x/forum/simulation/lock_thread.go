package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// SimulateMsgLockThread delivers MsgLockThread on the sentinel path: a
// CONTENT_SENTINEL holder (bonding one first through x/rep's MsgBondRole
// when no sim account holds the role) whose current bond covers the lock
// minimum (lock_bond_multiplier x min_sentinel_bond) and whose DREAM
// balance covers lock_backing_amount, under max_sentinel_locks_per_epoch and
// outside its overturn cooldown, locks a visible, unlocked thread it did not
// write and whose previous lock is not under appeal. The rep-tier gate
// (lock_min_rep_tier) and the bond reservation are left to the dry-run.
// With the default params (2000 DREAM bonded, 20000 DREAM backing, rep tier
// 4) sim sentinels rarely qualify, so when none does (and half the time
// otherwise) a Commons Operations Committee member locks on the council
// path instead, which reserves no bond and has no per-epoch cap.
func SimulateMsgLockThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgLockThread{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgLockThread{}
		p := e.params
		if p.ForumPaused || p.ModerationPaused {
			return noOp(msg, "forum or moderation paused")
		}
		sentinels, err := e.ensureSentinels()
		if err != nil {
			return noOpErr(msg, err)
		}
		minBond, backing := p.LockMinBondInt(), p.LockBackingAmountInt()
		sentinels = accountsWhere(sentinels, func(a simtypes.Account) bool {
			addr := a.Address.String()
			br, ok := e.bondedRole(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, addr)
			if !ok {
				return false
			}
			bond, ok := math.NewIntFromString(br.CurrentBond)
			m := e.loadMembers()[addr]
			return ok && bond.GTE(minBond) && m.DreamBalance != nil && m.DreamBalance.GTE(backing) &&
				e.sentinelActionsLeft(addr, reptypes.ActionKindForumLock, p.MaxSentinelLocksPerEpoch)
		})
		signer, ok := pick(e.r, sentinels)
		authority := types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL
		if council, has := pick(e.r, e.opsCommittee()); has && (!ok || e.r.Intn(2) == 0) {
			signer, ok = council, true
			authority = types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL
		}
		if !ok {
			return noOp(msg, "no sentinel with the lock bond and backing under its lock cap, and no committee member")
		}
		addr := signer.Address.String()
		roots, err := e.postsWhere(func(post types.Post) bool {
			if post.ParentId != 0 || !visible(post) || post.Locked {
				return false
			}
			// A sentinel may not moderate its own thread; the council path may.
			if authority == types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL && post.Author == addr {
				return false
			}
			lr, err := e.k.ThreadLockRecord.Get(e.ctx, post.PostId)
			return err != nil || !lr.AppealPending
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		root, ok := pick(e.r, roots)
		if !ok {
			return noOp(msg, "no unlocked thread the signer may lock")
		}
		msg.Creator, msg.RootId, msg.Reason = addr, root.PostId, "aurora: off-topic thread"
		msg.Authority = authority
		return e.submit(signer, msg, math.ZeroInt())
	})
}
