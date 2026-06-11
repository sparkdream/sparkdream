package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/types"
)

// rateHelpers exercises the unexported helper functions through their
// Keeper receivers. The fixture's keeper is the production keeper, so
// these tests cover the same code path as the msg handlers.

const testWindowSec int64 = 3600 // 1 hour

func TestCheckAndRecordInboundRate_Disabled(t *testing.T) {
	f := initFixture(t)

	// Zero limit: short-circuits, no state written.
	require.NoError(t, f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", 1000, testWindowSec, 0))
	// Non-positive window: also short-circuits.
	require.NoError(t, f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", 1000, 0, 5))
	require.NoError(t, f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", 1000, -1, 5))

	// No counter rows should have been written.
	count := 0
	require.NoError(t, f.keeper.InboundRateLimits.Walk(f.ctx, nil, func(_ collections.Pair[string, int64], _ uint64) (bool, error) {
		count++
		return false, nil
	}))
	require.Equal(t, 0, count)
}

func TestCheckAndRecordInboundRate_AcceptsUpToLimit(t *testing.T) {
	f := initFixture(t)

	const limit uint64 = 3
	blockTime := int64(10_000) // mid-window

	// First (limit) calls succeed.
	for i := uint64(0); i < limit; i++ {
		require.NoError(t, f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", blockTime, testWindowSec, limit))
	}
	// The next one is rejected.
	err := f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", blockTime, testWindowSec, limit)
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)

	// Counter advanced exactly `limit` times in the current window.
	windowStart := blockTime - (blockTime % testWindowSec)
	stored, err := f.keeper.InboundRateLimits.Get(f.ctx, collections.Join("p1", windowStart))
	require.NoError(t, err)
	require.Equal(t, limit, stored)
}

func TestCheckAndRecordInboundRate_PerPeerIsolated(t *testing.T) {
	f := initFixture(t)

	const limit uint64 = 2
	blockTime := int64(10_000)

	require.NoError(t, f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", blockTime, testWindowSec, limit))
	require.NoError(t, f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", blockTime, testWindowSec, limit))
	// p1 is full, p2 is fresh — p2 must still accept.
	require.NoError(t, f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p2", blockTime, testWindowSec, limit))
	// p1 still rejects.
	require.ErrorIs(t,
		f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", blockTime, testWindowSec, limit),
		types.ErrRateLimitExceeded,
	)
}

func TestCheckAndRecordInboundRate_PrevWindowBoundaryAttack(t *testing.T) {
	f := initFixture(t)

	const limit uint64 = 10
	// Pre-seed the previous window at capacity.
	prevWindowStart := int64(0)         // window covers [0, 3600)
	currentWindowStart := testWindowSec // current window starts at 3600
	require.NoError(t, f.keeper.InboundRateLimits.Set(f.ctx, collections.Join("p1", prevWindowStart), limit))

	// One second into the new window: remaining = window - 1 = 3599.
	// effective = current_count(0) + (prev_count(10) * 3599) / 3600 = 9 (floor).
	// 9 < 10, so this one passes — but the next should fail (current=1
	// + weighted prev=9 = 10, not < limit).
	blockTime := currentWindowStart + 1
	require.NoError(t, f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", blockTime, testWindowSec, limit))
	err := f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", blockTime, testWindowSec, limit)
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)
}

func TestCheckAndRecordInboundRate_PrevWindowFullyDecayed(t *testing.T) {
	f := initFixture(t)

	const limit uint64 = 5
	// Seed prev window at capacity.
	prevWindowStart := int64(0)
	require.NoError(t, f.keeper.InboundRateLimits.Set(f.ctx, collections.Join("p1", prevWindowStart), limit))

	// At the very last second of the current window, remainingSec is 1
	// — prev contributes floor(5 * 1 / 3600) = 0, so we get full `limit`
	// fresh accepts.
	blockTime := 2*testWindowSec - 1
	for i := uint64(0); i < limit; i++ {
		require.NoError(t, f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", blockTime, testWindowSec, limit))
	}
	require.ErrorIs(t,
		f.keeper.CallCheckAndRecordInboundRate(f.ctx, "p1", blockTime, testWindowSec, limit),
		types.ErrRateLimitExceeded,
	)
}

func TestCheckAndRecordOutboundRate_UsesOutboundStore(t *testing.T) {
	f := initFixture(t)

	const limit uint64 = 1
	blockTime := int64(10_000)

	require.NoError(t, f.keeper.CallCheckAndRecordOutboundRate(f.ctx, "p1", blockTime, testWindowSec, limit))
	require.ErrorIs(t,
		f.keeper.CallCheckAndRecordOutboundRate(f.ctx, "p1", blockTime, testWindowSec, limit),
		types.ErrRateLimitExceeded,
	)

	// Inbound store should remain empty (no cross-contamination).
	count := 0
	require.NoError(t, f.keeper.InboundRateLimits.Walk(f.ctx, nil, func(_ collections.Pair[string, int64], _ uint64) (bool, error) {
		count++
		return false, nil
	}))
	require.Equal(t, 0, count)
}

func TestCheckAndRecordPerBlock_Disabled(t *testing.T) {
	f := initFixture(t)
	// Zero limit short-circuits.
	require.NoError(t, f.keeper.CallCheckAndRecordInboundPerBlock(f.ctx, 100, 0))
	require.NoError(t, f.keeper.CallCheckAndRecordOutboundPerBlock(f.ctx, 100, 0))

	// No rows written.
	_, err := f.keeper.InboundPerBlock.Get(f.ctx, 100)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

func TestCheckAndRecordPerBlock_AcceptsUpToLimit(t *testing.T) {
	f := initFixture(t)

	const limit uint64 = 4
	for i := uint64(0); i < limit; i++ {
		require.NoError(t, f.keeper.CallCheckAndRecordInboundPerBlock(f.ctx, 100, limit))
	}
	require.ErrorIs(t,
		f.keeper.CallCheckAndRecordInboundPerBlock(f.ctx, 100, limit),
		types.ErrRateLimitExceeded,
	)

	stored, err := f.keeper.InboundPerBlock.Get(f.ctx, 100)
	require.NoError(t, err)
	require.Equal(t, limit, stored)
}

func TestCheckAndRecordPerBlock_NewBlockResets(t *testing.T) {
	f := initFixture(t)

	const limit uint64 = 2
	require.NoError(t, f.keeper.CallCheckAndRecordInboundPerBlock(f.ctx, 100, limit))
	require.NoError(t, f.keeper.CallCheckAndRecordInboundPerBlock(f.ctx, 100, limit))
	require.ErrorIs(t,
		f.keeper.CallCheckAndRecordInboundPerBlock(f.ctx, 100, limit),
		types.ErrRateLimitExceeded,
	)

	// Different height: starts fresh.
	require.NoError(t, f.keeper.CallCheckAndRecordInboundPerBlock(f.ctx, 101, limit))
	stored101, err := f.keeper.InboundPerBlock.Get(f.ctx, 101)
	require.NoError(t, err)
	require.Equal(t, uint64(1), stored101)
}

// EndBlocker prunes stale per-block entries strictly below current height.
func TestEndBlockerPrunesStalePerBlockCounters(t *testing.T) {
	f := initFixture(t)

	// Seed prior-height entries in both directions.
	require.NoError(t, f.keeper.InboundPerBlock.Set(f.ctx, int64(50), uint64(5)))
	require.NoError(t, f.keeper.InboundPerBlock.Set(f.ctx, int64(99), uint64(2)))
	require.NoError(t, f.keeper.OutboundPerBlock.Set(f.ctx, int64(75), uint64(3)))
	// Current-height entry — must survive.
	require.NoError(t, f.keeper.InboundPerBlock.Set(f.ctx, int64(100), uint64(1)))

	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100).WithBlockTime(time.Unix(10_000, 0))
	require.NoError(t, f.keeper.EndBlocker(sdkCtx))

	// Stale rows gone.
	_, err := f.keeper.InboundPerBlock.Get(sdkCtx, int64(50))
	require.ErrorIs(t, err, collections.ErrNotFound)
	_, err = f.keeper.InboundPerBlock.Get(sdkCtx, int64(99))
	require.ErrorIs(t, err, collections.ErrNotFound)
	_, err = f.keeper.OutboundPerBlock.Get(sdkCtx, int64(75))
	require.ErrorIs(t, err, collections.ErrNotFound)

	// Current-height row intact.
	stored, err := f.keeper.InboundPerBlock.Get(sdkCtx, int64(100))
	require.NoError(t, err)
	require.Equal(t, uint64(1), stored)
}
