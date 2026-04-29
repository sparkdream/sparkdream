package keeper

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// Compile-time assertion: Keeper satisfies the rep ForumKeeper contract.
var _ reptypes.ForumKeeper = Keeper{}

// PruneTagReferences scans every post and removes the given tag name from
// any post that references it. Invoked by x/rep's ResolveTagReport (action=1).
// Best-effort: iteration or write errors are surfaced but individual post
// update failures do not abort the scan.
func (k Keeper) PruneTagReferences(ctx context.Context, tagName string) error {
	iter, err := k.Post.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		post, err := iter.Value()
		if err != nil {
			continue
		}
		changed := false
		for i := 0; i < len(post.Tags); i++ {
			if post.Tags[i] == tagName {
				post.Tags = append(post.Tags[:i], post.Tags[i+1:]...)
				changed = true
				break
			}
		}
		if changed {
			_ = k.Post.Set(ctx, post.PostId, post)
		}
	}
	return nil
}

// GetPostAuthor returns the author address for the given post.
func (k Keeper) GetPostAuthor(ctx context.Context, postID uint64) (string, error) {
	post, err := k.Post.Get(ctx, postID)
	if err != nil {
		return "", fmt.Errorf("post %d not found: %w", postID, err)
	}
	return post.Author, nil
}

// GetPostTags returns the tag list for the given post.
func (k Keeper) GetPostTags(ctx context.Context, postID uint64) ([]string, error) {
	post, err := k.Post.Get(ctx, postID)
	if err != nil {
		return nil, fmt.Errorf("post %d not found: %w", postID, err)
	}
	return post.Tags, nil
}

// GetActionSentinel resolves the sentinel address that executed the given
// gov action from forum's own moderation records. Returns the empty string
// with no error if the record is missing (already garbage-collected) —
// callers should treat this as a soft skip.
func (k Keeper) GetActionSentinel(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) (string, error) {
	id, err := strconv.ParseUint(actionTarget, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid action target %q: %w", actionTarget, err)
	}

	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		rec, err := k.ThreadLockRecord.Get(ctx, id)
		if err != nil {
			return "", nil // missing — GC'd or never recorded
		}
		return rec.Sentinel, nil
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		rec, err := k.ThreadMoveRecord.Get(ctx, id)
		if err != nil {
			return "", nil
		}
		return rec.Sentinel, nil
	default:
		// Hide-like (post-level actions).
		rec, err := k.HideRecord.Get(ctx, id)
		if err != nil {
			return "", nil
		}
		return rec.Sentinel, nil
	}
}

// GetActionCommittedAmount returns the bond amount reserved by the sentinel
// for the given gov action. Hide actions record committed_amount on the
// HideRecord proto; lock/move actions reserve a flat DefaultSentinelSlashAmount.
// Returns zero (no error) when the record is missing — caller should treat as
// a soft skip.
func (k Keeper) GetActionCommittedAmount(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) (math.Int, error) {
	id, err := strconv.ParseUint(actionTarget, 10, 64)
	if err != nil {
		return math.ZeroInt(), fmt.Errorf("invalid action target %q: %w", actionTarget, err)
	}
	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		if _, err := k.ThreadLockRecord.Get(ctx, id); err != nil {
			return math.ZeroInt(), nil
		}
		return math.NewInt(types.DefaultSentinelSlashAmount), nil
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		if _, err := k.ThreadMoveRecord.Get(ctx, id); err != nil {
			return math.ZeroInt(), nil
		}
		return math.NewInt(types.DefaultSentinelSlashAmount), nil
	default:
		rec, err := k.HideRecord.Get(ctx, id)
		if err != nil {
			return math.ZeroInt(), nil
		}
		if rec.CommittedAmount == "" {
			return math.ZeroInt(), nil
		}
		v, ok := math.NewIntFromString(rec.CommittedAmount)
		if !ok {
			return math.ZeroInt(), nil
		}
		return v, nil
	}
}

// RecordSentinelActionUpheld updates forum's SentinelActivity counters when
// an appeal is resolved in the sentinel's favor. Increments the upheld_*
// counter (hide / lock / move) and consecutive_upheld, and resets
// consecutive_overturns. For hide actions, also decrements pending_hide_count.
// No-op (with log) when the sentinel cannot be resolved.
func (k Keeper) RecordSentinelActionUpheld(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	sentinel, err := k.GetActionSentinel(ctx, actionType, actionTarget)
	if err != nil {
		return err
	}
	if sentinel == "" {
		sdkCtx.Logger().Warn("record sentinel upheld: action record missing",
			"action_type", actionType.String(), "action_target", actionTarget)
		return nil
	}

	local, err := k.SentinelActivity.Get(ctx, sentinel)
	if err != nil {
		local = types.SentinelActivity{Address: sentinel}
	}

	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		local.UpheldLocks++
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		local.UpheldMoves++
	default:
		local.UpheldHides++
		if local.PendingHideCount > 0 {
			local.PendingHideCount--
		}
	}

	local.ConsecutiveUpheld++
	local.ConsecutiveOverturns = 0
	local.EpochAppealsResolved++

	if err := k.SentinelActivity.Set(ctx, sentinel, local); err != nil {
		return fmt.Errorf("persist sentinel activity (upheld): %w", err)
	}
	return nil
}

// RecordSentinelActionOverturned updates forum's SentinelActivity counters
// when an appeal is resolved against the sentinel. Increments overturned_*,
// increments consecutive_overturns, resets consecutive_upheld. If
// consecutive_overturns crosses DefaultMaxConsecutiveOverturnsBeforeDemotion,
// demotes the sentinel via the rep keeper. No-op (with log) when the sentinel
// cannot be resolved.
func (k Keeper) RecordSentinelActionOverturned(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	sentinel, err := k.GetActionSentinel(ctx, actionType, actionTarget)
	if err != nil {
		return err
	}
	if sentinel == "" {
		sdkCtx.Logger().Warn("record sentinel overturned: action record missing",
			"action_type", actionType.String(), "action_target", actionTarget)
		return nil
	}

	local, err := k.SentinelActivity.Get(ctx, sentinel)
	if err != nil {
		local = types.SentinelActivity{Address: sentinel}
	}

	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		local.OverturnedLocks++
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		local.OverturnedMoves++
	default:
		local.OverturnedHides++
		if local.PendingHideCount > 0 {
			local.PendingHideCount--
		}
	}

	local.ConsecutiveOverturns++
	local.ConsecutiveUpheld = 0
	local.EpochAppealsResolved++
	local.OverturnCooldownUntil = sdkCtx.BlockTime().Unix() + types.DefaultSentinelOverturnCooldown

	if err := k.SentinelActivity.Set(ctx, sentinel, local); err != nil {
		return fmt.Errorf("persist sentinel activity (overturned): %w", err)
	}

	if local.ConsecutiveOverturns >= reptypes.DefaultMaxConsecutiveOverturnsBeforeDemotion {
		if k.repKeeper == nil {
			sdkCtx.Logger().Warn("cannot demote sentinel: rep keeper not wired",
				"sentinel", sentinel)
			return nil
		}
		cooldownUntil := sdkCtx.BlockTime().Unix() + reptypes.DefaultSentinelDemotionCooldown
		if err := k.repKeeper.SetBondStatus(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, sentinel,
			reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, cooldownUntil); err != nil {
			sdkCtx.Logger().Error("failed to demote sentinel after overturn streak",
				"sentinel", sentinel, "consecutive_overturns", local.ConsecutiveOverturns,
				"error", err)
		}
	}
	return nil
}

// GetSentinelActivityCounters loads forum's per-sentinel counter record and
// returns the subset needed by x/rep's reward distribution logic.
// Missing record -> zero-valued struct with no error.
func (k Keeper) GetSentinelActivityCounters(ctx context.Context, addr string) (reptypes.SentinelActivityCounters, error) {
	local, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return reptypes.SentinelActivityCounters{}, nil
	}
	return reptypes.SentinelActivityCounters{
		UpheldHides:          local.UpheldHides,
		OverturnedHides:      local.OverturnedHides,
		UpheldLocks:          local.UpheldLocks,
		OverturnedLocks:      local.OverturnedLocks,
		UpheldMoves:          local.UpheldMoves,
		OverturnedMoves:      local.OverturnedMoves,
		EpochHides:           local.EpochHides,
		EpochLocks:           local.EpochLocks,
		EpochMoves:           local.EpochMoves,
		EpochPins:            local.EpochPins,
		EpochAppealsFiled:    local.EpochAppealsFiled,
		EpochAppealsResolved: local.EpochAppealsResolved,
	}, nil
}

// ResetSentinelEpochCounters zeros the per-epoch counters on the forum-side
// SentinelActivity record, preserving cumulative counters. Called by x/rep at
// sentinel-reward epoch boundaries. Missing record -> no-op.
func (k Keeper) ResetSentinelEpochCounters(ctx context.Context, addr string) error {
	local, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return nil
	}
	local.EpochHides = 0
	local.EpochLocks = 0
	local.EpochMoves = 0
	local.EpochPins = 0
	local.EpochAppealsFiled = 0
	local.EpochAppealsResolved = 0
	return k.SentinelActivity.Set(ctx, addr, local)
}
