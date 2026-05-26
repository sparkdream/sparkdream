package keeper

import (
	"context"
	"errors"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
)

// checkAndRecordInboundRate enforces and increments the per-peer
// sliding-window rate limit for inbound content (spec §10.2). A zero
// limit or non-positive window disables the check.
func (k Keeper) checkAndRecordInboundRate(
	ctx context.Context,
	peerID string,
	blockTime, windowSec int64,
	limit uint64,
) error {
	return checkAndRecordPeerRate(ctx, k.InboundRateLimits, peerID, blockTime, windowSec, limit)
}

// checkAndRecordOutboundRate is the symmetric helper for outbound
// federation (spec §10.3).
func (k Keeper) checkAndRecordOutboundRate(
	ctx context.Context,
	peerID string,
	blockTime, windowSec int64,
	limit uint64,
) error {
	return checkAndRecordPeerRate(ctx, k.OutboundRateLimits, peerID, blockTime, windowSec, limit)
}

// checkAndRecordInboundPerBlock enforces and increments the global
// per-block inbound cap (spec §10.2). Zero limit disables the check.
func (k Keeper) checkAndRecordInboundPerBlock(ctx context.Context, height int64, limit uint64) error {
	return checkAndRecordPerBlockCap(ctx, k.InboundPerBlock, height, limit)
}

// checkAndRecordOutboundPerBlock is the symmetric helper for outbound
// federation (spec §10.3).
func (k Keeper) checkAndRecordOutboundPerBlock(ctx context.Context, height int64, limit uint64) error {
	return checkAndRecordPerBlockCap(ctx, k.OutboundPerBlock, height, limit)
}

func checkAndRecordPerBlockCap(
	ctx context.Context,
	store collections.Map[int64, uint64],
	height int64,
	limit uint64,
) error {
	if limit == 0 {
		return nil
	}
	count, err := store.Get(ctx, height)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if count >= limit {
		return types.ErrRateLimitExceeded
	}
	return store.Set(ctx, height, count+1)
}

func checkAndRecordPeerRate(
	ctx context.Context,
	store collections.Map[collections.Pair[string, int64], uint64],
	peerID string,
	blockTime, windowSec int64,
	limit uint64,
) error {
	if limit == 0 || windowSec <= 0 {
		return nil
	}

	currentWindowStart := blockTime - (blockTime % windowSec)
	prevWindowStart := currentWindowStart - windowSec

	currentCount, err := store.Get(ctx, collections.Join(peerID, currentWindowStart))
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	prevCount, err := store.Get(ctx, collections.Join(peerID, prevWindowStart))
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}

	// Integer-only sliding-window approximation (spec §10.2):
	// effective = current + (prev * remaining) / window
	// All division is floor; matches the LegacyDec-style fixed-point
	// pattern used elsewhere for consensus-deterministic arithmetic.
	remainingSec := uint64(windowSec - (blockTime - currentWindowStart))
	effective := currentCount + (prevCount*remainingSec)/uint64(windowSec)
	if effective >= limit {
		return types.ErrRateLimitExceeded
	}

	return store.Set(ctx, collections.Join(peerID, currentWindowStart), currentCount+1)
}
