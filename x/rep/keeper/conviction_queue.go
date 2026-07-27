package keeper

import (
	"context"
	"errors"
	"fmt"
	"math"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ---------------------------------------------------------------------------
// Bounded, incremental conviction refresh
// ---------------------------------------------------------------------------
//
// Initiative conviction used to be recomputed for every active initiative on
// every block, reading every stake and (before the GetMember fix) writing every
// staker's member record each time. That made per-block validator work scale
// linearly with a stake count that any member could inflate for a refundable
// cost — a one-time fee bought permanent per-block work.
//
// Conviction is now driven by a due-time-ordered work queue:
//
//   - Anything that can change conviction reschedules the affected initiative
//     to "due now": a stake mutation on the initiative itself, or a stake
//     mutation on content linked to it (via the InitiativesByContent reverse
//     index).
//   - Conviction also drifts on its own, because each stake's time weighting
//     climbs until it matures. An initiative therefore re-arms itself after
//     every recompute, at a cadence derived from the maturity window while any
//     of its stakes is still maturing, and a long one once they all have.
//   - EndBlocker drains only the due prefix, under a per-block budget counted
//     in stakes. Work that does not fit rolls to the next block.
//
// The result: a block's conviction work is capped by a constant regardless of
// how many stakes exist, and dust stakes stop costing anything once matured.
// Saturating the queue delays conviction freshness; it cannot inflate block
// time. Note that the InitiativeConviction query recomputes on demand, so a
// queued initiative never serves a stale figure to a caller who asks directly.

const (
	// MaxConvictionStakeUpdatesPerBlock caps how many stake-level conviction
	// recomputations EndBlocker performs in one block. An initiative is always
	// processed whole (conviction is meaningless from a partial stake set), so
	// the budget is checked between initiatives and one oversized initiative may
	// overshoot it — that is bounded by the per-target tranche cap times the
	// number of distinct stakers.
	//
	// This is a compile-time constant rather than a governance param on purpose.
	// It bounds unmetered validator work in EndBlocker, so a value set too high
	// is itself a liveness risk; changing it belongs to a chain upgrade
	// alongside any change to the sweep's cost model. Matches the local
	// convention (see maxTagExpirations in abci.go).
	MaxConvictionStakeUpdatesPerBlock = 500

	// ConvictionStableRefreshSeconds is how long an initiative whose stakes have
	// all fully matured may go without a recompute. Such an initiative's
	// conviction only drifts via reputation decay (0.1%/epoch by default), so a
	// multi-hour cadence keeps the error negligible while removing matured
	// stakes from the per-block cost model almost entirely.
	ConvictionStableRefreshSeconds int64 = 6 * 60 * 60

	// minConvictionRefreshSeconds floors the maturing-phase cadence so a
	// pathologically short conviction half-life cannot degenerate back into a
	// per-block sweep.
	minConvictionRefreshSeconds int64 = 60

	// convictionRefreshesPerHalfLife sets how many recomputes are scheduled
	// across a stake's maturity window. Deriving the cadence from the half-life
	// keeps conviction tracking maturity at the same fidelity regardless of how
	// conviction_half_life_epochs and epoch_blocks are configured.
	convictionRefreshesPerHalfLife int64 = 8
)

// ScheduleConvictionRefresh (re)arms an initiative's conviction recompute for
// `dueAt`, replacing any entry it already has in the queue. Passing a due time
// at or before the current block time makes it eligible on the next EndBlocker.
func (k Keeper) ScheduleConvictionRefresh(ctx context.Context, initiativeID uint64, dueAt int64) error {
	existing, err := k.ConvictionScheduledAt.Get(ctx, initiativeID)
	switch {
	case err == nil:
		if existing == dueAt {
			return nil
		}
		if err := k.ConvictionQueue.Remove(ctx, collections.Join(existing, initiativeID)); err != nil {
			return err
		}
	case errors.Is(err, collections.ErrNotFound):
		// Not scheduled yet.
	default:
		return err
	}

	if err := k.ConvictionQueue.Set(ctx, collections.Join(dueAt, initiativeID)); err != nil {
		return err
	}
	return k.ConvictionScheduledAt.Set(ctx, initiativeID, dueAt)
}

// MarkConvictionDirty schedules an immediate recompute for an initiative whose
// conviction inputs just changed.
func (k Keeper) MarkConvictionDirty(ctx context.Context, initiativeID uint64) error {
	return k.ScheduleConvictionRefresh(ctx, initiativeID, sdk.UnwrapSDKContext(ctx).BlockTime().Unix())
}

// MarkConvictionDirtyForContent schedules an immediate recompute for every
// initiative that links the given content item. Content conviction propagates
// into linked initiatives, so a content stake mutation changes their conviction
// without touching any of their own stakes.
func (k Keeper) MarkConvictionDirtyForContent(ctx context.Context, targetType types.StakeTargetType, targetID uint64) error {
	contentKey := collections.Join(int32(targetType), targetID)
	rng := collections.NewPrefixedPairRange[collections.Pair[int32, uint64], uint64](contentKey)

	var initiativeIDs []uint64
	if err := k.InitiativesByContent.Walk(ctx, rng, func(key collections.Pair[collections.Pair[int32, uint64], uint64]) (bool, error) {
		initiativeIDs = append(initiativeIDs, key.K2())
		return false, nil
	}); err != nil {
		return err
	}

	// Mutate after the iterator closes.
	for _, id := range initiativeIDs {
		if err := k.MarkConvictionDirty(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// refreshConvictionForStake brings conviction up to date for whatever the given
// stake feeds into, and leaves the affected initiatives correctly armed on the
// queue. Called from both stake mutation sites.
//
// An initiative stake is recomputed synchronously: the work is gas-metered on
// the staker's own transaction and they should see the effect immediately. A
// content stake is not — it can fan out to many linked initiatives, so those are
// marked due and picked up by the next EndBlocker under its work budget.
func (k Keeper) refreshConvictionForStake(ctx context.Context, stake types.Stake) error {
	switch {
	case stake.TargetType == types.StakeTargetType_STAKE_TARGET_INITIATIVE:
		if err := k.UpdateInitiativeConvictionLazy(ctx, stake.TargetId); err != nil {
			return fmt.Errorf("failed to update conviction: %w", err)
		}
		// Already current as of this block, so arm it for its next natural
		// refresh rather than marking it due now and recomputing twice.
		now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
		nextDue, err := k.nextConvictionRefreshAt(ctx, stake.TargetId, now)
		if err != nil {
			return err
		}
		return k.ScheduleConvictionRefresh(ctx, stake.TargetId, nextDue)

	case types.IsContentConvictionType(stake.TargetType):
		// Content conviction propagates into every initiative that links this
		// item. Without the reverse-index lookup here, an incremental refresh
		// would never notice a content stake at all.
		return k.MarkConvictionDirtyForContent(ctx, stake.TargetType, stake.TargetId)
	}

	// Member, tag, project and author bond stakes carry no conviction.
	return nil
}

// UnscheduleConvictionRefresh drops an initiative from the queue entirely. Used
// when an initiative reaches a terminal state and can no longer accrue.
func (k Keeper) UnscheduleConvictionRefresh(ctx context.Context, initiativeID uint64) error {
	existing, err := k.ConvictionScheduledAt.Get(ctx, initiativeID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := k.ConvictionQueue.Remove(ctx, collections.Join(existing, initiativeID)); err != nil {
		return err
	}
	return k.ConvictionScheduledAt.Remove(ctx, initiativeID)
}

// RearmConvictionQueue marks every initiative that can still accrue conviction
// as due. The queue is derived state and is not carried in genesis, so this
// rebuilds it on import. Also usable as a recovery hatch if the queue is ever
// suspected of having drifted.
//
// This is O(active initiatives) and touches no stakes; the recomputation itself
// still happens under the EndBlocker budget, spread across as many blocks as it
// takes.
func (k Keeper) RearmConvictionQueue(ctx context.Context) error {
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	var ids []uint64
	k.IterateActiveInitiatives(ctx, func(_ int64, initiative types.Initiative) bool {
		ids = append(ids, initiative.Id)
		return false
	})

	for _, id := range ids {
		if err := k.ScheduleConvictionRefresh(ctx, id, now); err != nil {
			return err
		}
	}
	return nil
}

// DrainConvictionQueue recomputes conviction for initiatives whose refresh is
// due, stopping once the per-block stake budget is spent. Each processed
// initiative re-arms itself for its next due time; anything left over stays
// queued and is picked up by a later block.
func (k Keeper) DrainConvictionQueue(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Only scan the due prefix. Entries scheduled for the future are ordered
	// after this bound and are never read.
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(now, uint64(math.MaxUint64)))

	type dueEntry struct {
		dueAt        int64
		initiativeID uint64
	}
	var due []dueEntry

	// Collect first, mutate after the iterator closes. The scan itself is
	// bounded by the same budget so a large backlog cannot make the walk
	// expensive even before any recomputation happens.
	if err := k.ConvictionQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if len(due) >= MaxConvictionStakeUpdatesPerBlock {
			return true, nil
		}
		due = append(due, dueEntry{dueAt: key.K1(), initiativeID: key.K2()})
		return false, nil
	}); err != nil {
		return err
	}

	spent := 0
	for _, entry := range due {
		// Budget is checked between initiatives: conviction cannot be computed
		// from a partial stake set, so an initiative is never split across
		// blocks. Always process at least one so a single initiative larger than
		// the whole budget cannot wedge the queue forever.
		if spent >= MaxConvictionStakeUpdatesPerBlock {
			break
		}

		stakeCount, err := k.updateInitiativeConvictionCounted(ctx, entry.initiativeID)
		if err != nil {
			// A queued initiative that no longer exists (or whose project is
			// gone) must still leave the queue, or it would be retried forever.
			sdkCtx.Logger().Debug("dropping unprocessable conviction queue entry",
				"initiative_id", entry.initiativeID, "error", err)
			if err := k.UnscheduleConvictionRefresh(ctx, entry.initiativeID); err != nil {
				return err
			}
			spent++
			continue
		}

		// Charge at least one unit even for a stake-less initiative, so an
		// initiative with no stakes still consumes budget rather than letting an
		// unbounded number through per block.
		if stakeCount < 1 {
			stakeCount = 1
		}
		spent += stakeCount

		// Retire the exact entry that was processed. If something rescheduled
		// this initiative after the walk collected it, the reverse pointer no
		// longer matches and the newer entry is already correctly armed — leave
		// it alone rather than clobbering it.
		scheduled, err := k.ConvictionScheduledAt.Get(ctx, entry.initiativeID)
		if err != nil || scheduled != entry.dueAt {
			if err != nil && !errors.Is(err, collections.ErrNotFound) {
				return err
			}
			// Stale queue entry with no matching pointer: drop it so it cannot
			// be re-processed every block.
			if err := k.ConvictionQueue.Remove(ctx, collections.Join(entry.dueAt, entry.initiativeID)); err != nil {
				return err
			}
			continue
		}
		if err := k.ConvictionQueue.Remove(ctx, collections.Join(entry.dueAt, entry.initiativeID)); err != nil {
			return err
		}
		if err := k.ConvictionScheduledAt.Remove(ctx, entry.initiativeID); err != nil {
			return err
		}

		// An initiative that has left the active set can no longer accrue
		// conviction, so it stays off the queue. Checking status here rather
		// than unscheduling at each terminal transition means completion,
		// cancellation, abandonment and expiry are all covered without needing a
		// hook in each one.
		active, err := k.convictionStillAccrues(ctx, entry.initiativeID)
		if err != nil {
			return err
		}
		if !active {
			continue
		}

		nextDue, err := k.nextConvictionRefreshAt(ctx, entry.initiativeID, now)
		if err != nil {
			return err
		}
		if err := k.ScheduleConvictionRefresh(ctx, entry.initiativeID, nextDue); err != nil {
			return err
		}
	}

	return nil
}

// convictionStillAccrues reports whether an initiative is in a status where
// conviction can still change. Mirrors the active set IterateActiveInitiatives
// walks.
func (k Keeper) convictionStillAccrues(ctx context.Context, initiativeID uint64) (bool, error) {
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	switch initiative.Status {
	case types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
		types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED,
		types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED,
		types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW,
		types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED:
		return true, nil
	default:
		return false, nil
	}
}

// nextConvictionRefreshAt returns when an initiative's conviction could next
// change without any external event.
//
// While any stake is still maturing, conviction climbs continuously and is
// re-armed at a fraction of the maturity window. Once every stake has reached
// full maturity, the only remaining drift is reputation decay, which is slow
// enough to check a few times a day.
func (k Keeper) nextConvictionRefreshAt(ctx context.Context, initiativeID uint64, now int64) (int64, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	// Mirrors CalculateRawStakeConviction: a stake is fully matured once
	// elapsed >= 2 * halfLife, after which its time factor is pinned at 1.
	halfLifeSeconds := int64(params.ConvictionHalfLifeEpochs * params.EpochBlocks * 6)
	if halfLifeSeconds <= 0 {
		return now + ConvictionStableRefreshSeconds, nil
	}
	maturityWindow := 2 * halfLifeSeconds

	stakes, err := k.GetInitiativeStakes(ctx, initiativeID)
	if err != nil {
		return 0, err
	}

	maturing := false
	for _, stake := range stakes {
		if now-stake.CreatedAt < maturityWindow {
			maturing = true
			break
		}
	}

	if !maturing {
		return now + ConvictionStableRefreshSeconds, nil
	}

	interval := halfLifeSeconds / convictionRefreshesPerHalfLife
	if interval < minConvictionRefreshSeconds {
		interval = minConvictionRefreshSeconds
	}
	return now + interval, nil
}

// updateInitiativeConvictionCounted recomputes an initiative's conviction and
// reports how many stakes it read, so the caller can charge a work budget. The
// stake slice is fetched once and passed through, not read twice.
func (k Keeper) updateInitiativeConvictionCounted(ctx context.Context, initiativeID uint64) (int, error) {
	stakes, err := k.GetInitiativeStakes(ctx, initiativeID)
	if err != nil {
		return 0, err
	}
	if err := k.updateInitiativeConvictionWithStakes(ctx, initiativeID, stakes); err != nil {
		return len(stakes), err
	}
	return len(stakes), nil
}
