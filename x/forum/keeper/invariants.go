package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
)

// RegisterInvariants registers all forum module invariants.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "post-counter", PostCounterInvariant(k))
	ir.RegisterRoute(types.ModuleName, "bounty-post-reference", BountyPostReferenceInvariant(k))
	ir.RegisterRoute(types.ModuleName, "sentinel-bond-status", SentinelBondStatusInvariant(k))
	ir.RegisterRoute(types.ModuleName, "thread-lock-consistency", ThreadLockConsistencyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "hide-record-consistency", HideRecordConsistencyInvariant(k))
}

// PostCounterInvariant checks that the PostSeq counter is greater than every
// stored post ID (no post ID >= the counter value).
func PostCounterInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		postSeq, err := k.PostSeq.Peek(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "post-counter",
				fmt.Sprintf("failed to read post sequence: %v", err)), true
		}

		var broken int
		var msg string

		err = k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
			if post.PostId >= postSeq {
				broken++
				msg += fmt.Sprintf("  post ID %d >= PostSeq %d\n", post.PostId, postSeq)
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "post-counter",
				fmt.Sprintf("error walking posts: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "post-counter",
			fmt.Sprintf("found %d post counter violations\n%s", broken, msg)), broken > 0
	}
}

// BountyPostReferenceInvariant checks that every active bounty references
// an existing root post.
func BountyPostReferenceInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.Bounty.Walk(ctx, nil, func(id uint64, bounty types.Bounty) (bool, error) {
			// Only check active bounties
			if bounty.Status != types.BountyStatus_BOUNTY_STATUS_ACTIVE {
				return false, nil
			}

			_, err := k.Post.Get(ctx, bounty.ThreadId)
			if err != nil {
				broken++
				msg += fmt.Sprintf("  bounty %d references non-existent root post %d\n",
					id, bounty.ThreadId)
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "bounty-post-reference",
				fmt.Sprintf("error walking bounties: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "bounty-post-reference",
			fmt.Sprintf("found %d bounty reference violations\n%s", broken, msg)), broken > 0
	}
}

// SentinelBondStatusInvariant checks that each sentinel's BondStatus is
// consistent with their CurrentBond amount:
//   - NORMAL: bond >= 1000
//   - RECOVERY: 500 <= bond < 1000
//   - DEMOTED: bond < 500
//   - CommittedBond <= CurrentBond
func SentinelBondStatusInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		normalMin := math.NewInt(1000)
		recoveryMin := math.NewInt(500)

		err := k.SentinelActivity.Walk(ctx, nil, func(addr string, sa types.SentinelActivity) (bool, error) {
			currentBond, ok := math.NewIntFromString(sa.CurrentBond)
			if !ok || sa.CurrentBond == "" {
				currentBond = math.ZeroInt()
			}
			committedBond, ok := math.NewIntFromString(sa.TotalCommittedBond)
			if !ok || sa.TotalCommittedBond == "" {
				committedBond = math.ZeroInt()
			}

			// CommittedBond must not exceed CurrentBond
			if committedBond.GT(currentBond) {
				broken++
				msg += fmt.Sprintf("  sentinel %s: committed_bond %s > current_bond %s\n",
					addr, committedBond.String(), currentBond.String())
			}

			// Verify bond status consistency
			switch sa.BondStatus {
			case types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL:
				if currentBond.LT(normalMin) {
					broken++
					msg += fmt.Sprintf("  sentinel %s: NORMAL status but bond %s < 1000\n",
						addr, currentBond.String())
				}
			case types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY:
				if currentBond.GTE(normalMin) {
					broken++
					msg += fmt.Sprintf("  sentinel %s: RECOVERY status but bond %s >= 1000\n",
						addr, currentBond.String())
				}
				if currentBond.LT(recoveryMin) {
					broken++
					msg += fmt.Sprintf("  sentinel %s: RECOVERY status but bond %s < 500 (should be DEMOTED)\n",
						addr, currentBond.String())
				}
			case types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED:
				if currentBond.GTE(recoveryMin) {
					broken++
					msg += fmt.Sprintf("  sentinel %s: DEMOTED status but bond %s >= 500\n",
						addr, currentBond.String())
				}
			}

			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "sentinel-bond-status",
				fmt.Sprintf("error walking sentinel activities: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "sentinel-bond-status",
			fmt.Sprintf("found %d sentinel bond status violations\n%s", broken, msg)), broken > 0
	}
}

// ThreadLockConsistencyInvariant checks that every ThreadLockRecord
// references an existing root post that has its Locked field set to true.
func ThreadLockConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.ThreadLockRecord.Walk(ctx, nil, func(rootID uint64, tlr types.ThreadLockRecord) (bool, error) {
			post, err := k.Post.Get(ctx, rootID)
			if err != nil {
				broken++
				msg += fmt.Sprintf("  lock record for non-existent root post %d\n", rootID)
				return false, nil
			}
			if !post.Locked {
				broken++
				msg += fmt.Sprintf("  lock record exists for root post %d but post.Locked=false\n", rootID)
			}
			if post.ParentId != 0 {
				broken++
				msg += fmt.Sprintf("  lock record references non-root post %d (parent_id=%d)\n",
					rootID, post.ParentId)
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "thread-lock-consistency",
				fmt.Sprintf("error walking thread lock records: %v", err)), true
		}

		// Also check the reverse: any locked post should have a lock record
		err = k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
			if post.Locked && post.ParentId == 0 {
				_, err := k.ThreadLockRecord.Get(ctx, id)
				if err != nil {
					if errors.Is(err, collections.ErrNotFound) {
						broken++
						msg += fmt.Sprintf("  post %d is locked but has no ThreadLockRecord\n", id)
					}
				}
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "thread-lock-consistency",
				fmt.Sprintf("error walking posts for lock check: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "thread-lock-consistency",
			fmt.Sprintf("found %d thread lock consistency violations\n%s", broken, msg)), broken > 0
	}
}

// HideRecordConsistencyInvariant checks that every HideRecord references
// an existing post that is in HIDDEN status.
func HideRecordConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.HideRecord.Walk(ctx, nil, func(postID uint64, hr types.HideRecord) (bool, error) {
			post, err := k.Post.Get(ctx, postID)
			if err != nil {
				broken++
				msg += fmt.Sprintf("  hide record for non-existent post %d\n", postID)
				return false, nil
			}
			// HideRecord should only exist for HIDDEN posts (or posts pending appeal)
			if post.Status != types.PostStatus_POST_STATUS_HIDDEN {
				broken++
				msg += fmt.Sprintf("  hide record exists for post %d but status is %s\n",
					postID, post.Status.String())
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "hide-record-consistency",
				fmt.Sprintf("error walking hide records: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "hide-record-consistency",
			fmt.Sprintf("found %d hide record consistency violations\n%s", broken, msg)), broken > 0
	}
}
