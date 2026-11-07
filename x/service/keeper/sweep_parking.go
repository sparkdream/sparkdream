package keeper

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// sweepRetryBlocks is how long an EndBlocker item whose attempt failed is
// parked before it is tried again. A failing item was retried
// every block at its queue position before: enough of them (one batch,
// endblocker_sweep_limit) filled every block's batch, and nothing queued
// behind them was ever processed, while the same failing writes were redone
// each block. Parked, a failing item costs one attempt per window.
const sweepRetryBlocks int64 = 100

// sweepScanFactor bounds how many queue entries a sweep reads per block,
// parked ones included, as a multiple of its batch limit. Skipping a parked
// entry is a single read, so a sweep only stalls behind more than
// limit * sweepScanFactor items failing at once.
const sweepScanFactor = 10

// Sweep names, as runIsolated and the parks record them.
const (
	sweepUnderfundedName             = "sweepUnderfunded"
	sweepPendingReportsName          = "sweepPendingReports"
	sweepEscalatedReportsName        = "sweepEscalatedReports"
	sweepControllerTransferCasesName = "sweepControllerTransferCases"
	sweepTier1EscrowReleaseName      = "sweepTier1EscrowRelease"
	sweepCheckpointLivenessName      = "checkpoint_liveness"
	sweepSystemReportDedupName       = "system_report_dedup"
)

// idItem names a record keyed by a numeric id (report, case, escrow).
func idItem(id uint64) string {
	return strconv.FormatUint(id, 10)
}

// operatorItem names an (operator, service_type) record.
func operatorItem(addr []byte, serviceType string) string {
	return fmt.Sprintf("%x/%s", addr, serviceType)
}

// dedupItem names a (caller_module, dedupe_key) dedup entry.
func dedupItem(callerModule string, dedupeKey []byte) string {
	return fmt.Sprintf("%s/%x", callerModule, dedupeKey)
}

// dueItem prefixes an item with the due height of its queue entry. Queues
// whose entry for the same record is re-filed at a new height (a new
// liveness deadline, a new underfunded grace, a re-queued dedup entry) key
// their parks by the entry, so a re-filed entry never inherits the park of
// the one it replaced.
func dueItem(height int64, item string) string {
	return fmt.Sprintf("%d/%s", height, item)
}

// sweepScanCap is the number of queue entries a sweep may read this block.
func sweepScanCap(limit int) int {
	return limit * sweepScanFactor
}

// isSweepParked reports whether item of sweep is parked past height.
func (k Keeper) isSweepParked(ctx context.Context, sweep, item string, height int64) bool {
	at, err := k.SweepParkedAt.Get(ctx, collections.Join(sweep, item))
	return err == nil && at > height
}

// parkSweepItem parks item of sweep until height + sweepRetryBlocks.
// Called on the parent context after the item's cache context was
// discarded, so the park survives the rollback. Best effort: a failed write
// leaves the item retried next block, as before.
func (k Keeper) parkSweepItem(ctx context.Context, sweep, item string, height int64) {
	k.unparkSweepItem(ctx, sweep, item)
	retryAt := height + sweepRetryBlocks
	logger := sdk.UnwrapSDKContext(ctx).Logger()
	if err := k.SweepParked.Set(ctx, collections.Join3(retryAt, sweep, item)); err != nil {
		logger.Error("parkSweepItem: SweepParked.Set failed", "sweep", sweep, "item", item, "err", err)
		return
	}
	if err := k.SweepParkedAt.Set(ctx, collections.Join(sweep, item), retryAt); err != nil {
		logger.Error("parkSweepItem: SweepParkedAt.Set failed", "sweep", sweep, "item", item, "err", err)
	}
}

// unparkSweepItem drops item's park, if any.
func (k Keeper) unparkSweepItem(ctx context.Context, sweep, item string) {
	key := collections.Join(sweep, item)
	at, err := k.SweepParkedAt.Get(ctx, key)
	if err != nil {
		return
	}
	logger := sdk.UnwrapSDKContext(ctx).Logger()
	if err := k.SweepParked.Remove(ctx, collections.Join3(at, sweep, item)); err != nil {
		logger.Error("unparkSweepItem: SweepParked.Remove failed", "sweep", sweep, "item", item, "err", err)
	}
	if err := k.SweepParkedAt.Remove(ctx, key); err != nil {
		logger.Error("unparkSweepItem: SweepParkedAt.Remove failed", "sweep", sweep, "item", item, "err", err)
	}
}

// sweepExpiredParks drops parks whose retry height has come: an expired park
// no longer holds anything back, and one whose item left its queue another
// way (a report resolved by message) would otherwise stay in state for good.
// Capped at sweepScanCap(limit) per block, so it keeps up with every sweep
// parking a full batch at once.
func (k Keeper) sweepExpiredParks(ctx context.Context, currentHeight int64, limit int) (int, error) {
	var due []collections.Triple[int64, string, string]
	if err := k.SweepParked.Walk(ctx, collections.NewPrefixUntilTripleRange[int64, string, string](currentHeight),
		func(key collections.Triple[int64, string, string]) (bool, error) {
			due = append(due, key)
			return len(due) >= limit, nil
		}); err != nil {
		return 0, err
	}
	for _, key := range due {
		if err := k.SweepParked.Remove(ctx, key); err != nil {
			return 0, err
		}
		revKey := collections.Join(key.K2(), key.K3())
		if at, err := k.SweepParkedAt.Get(ctx, revKey); err == nil && at == key.K1() {
			if err := k.SweepParkedAt.Remove(ctx, revKey); err != nil {
				return 0, err
			}
		}
	}
	return len(due), nil
}
