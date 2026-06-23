package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// hideTarget seeds a HideRecord so GetActionSentinel resolves `sentinel` for a
// default/WARNING (hide) action against postID, then returns the target string.
func hideTarget(t *testing.T, f *fixture, sentinel string, postID uint64) string {
	t.Helper()
	require.NoError(t, f.keeper.HideRecord.Set(f.ctx, postID, types.HideRecord{PostId: postID, Sentinel: sentinel}))
	return fmt.Sprintf("%d", postID)
}

func recordUpheld(t *testing.T, f *fixture, epoch, postID uint64, sentinel string) {
	t.Helper()
	tgt := hideTarget(t, f, sentinel, postID)
	require.NoError(t, f.keeper.RecordSentinelActionUpheld(f.ctx, epoch, reptypes.GovActionType_GOV_ACTION_TYPE_WARNING, tgt))
}

func recordOverturned(t *testing.T, f *fixture, epoch, postID uint64, sentinel string) {
	t.Helper()
	tgt := hideTarget(t, f, sentinel, postID)
	require.NoError(t, f.keeper.RecordSentinelActionOverturned(f.ctx, epoch, reptypes.GovActionType_GOV_ACTION_TYPE_WARNING, tgt))
}

// TestAccuracyWindow_RingSizeMatchesRepCap guards the cross-module invariant
// that the forum ring has at least as many slots as the largest window x/rep
// will ask for. If these drift, the ring could be asked to resolve a window
// longer than it can store.
func TestAccuracyWindow_RingSizeMatchesRepCap(t *testing.T) {
	require.Equal(t, uint64(types.SentinelAccuracyRingSize), reptypes.MaxSentinelAccuracyWindowEpochs)
}

// TestAccuracyWindow_RecordAndRead verifies upheld/overturned land in the
// correct epoch buckets and that the windowed read sums only the requested span.
func TestAccuracyWindow_RecordAndRead(t *testing.T) {
	f := initFixture(t)
	const sentinel = "window-sentinel"

	recordUpheld(t, f, 5, 10, sentinel)
	recordUpheld(t, f, 5, 11, sentinel)
	recordOverturned(t, f, 6, 12, sentinel)

	// Window covering epochs [1..6] sees everything.
	up, ov, err := f.keeper.GetSentinelWindowedAccuracy(f.ctx, sentinel, 6, 6)
	require.NoError(t, err)
	require.Equal(t, uint64(2), up)
	require.Equal(t, uint64(1), ov)

	// Window of 1 ending at epoch 6 sees only epoch 6.
	up, ov, err = f.keeper.GetSentinelWindowedAccuracy(f.ctx, sentinel, 6, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(0), up)
	require.Equal(t, uint64(1), ov)
}

// TestAccuracyWindow_GapNoLeak verifies that an inactive epoch in the middle of
// the window does not leak older tallies into the current read.
func TestAccuracyWindow_GapNoLeak(t *testing.T) {
	f := initFixture(t)
	const sentinel = "gap-sentinel"

	recordUpheld(t, f, 1, 10, sentinel)     // epoch 1
	recordOverturned(t, f, 3, 11, sentinel) // epoch 3 (epoch 2 inactive)

	// Window of 2 ending at epoch 3 covers [2,3] -> epoch 1 excluded.
	up, ov, err := f.keeper.GetSentinelWindowedAccuracy(f.ctx, sentinel, 3, 2)
	require.NoError(t, err)
	require.Equal(t, uint64(0), up)
	require.Equal(t, uint64(1), ov)
}

// TestAccuracyWindow_StaleSlotEvicted verifies that when an epoch wraps around
// to a slot already occupied by an out-of-window epoch (epoch and epoch+ringsize
// share a slot), the stale tally is dropped rather than counted.
func TestAccuracyWindow_StaleSlotEvicted(t *testing.T) {
	f := initFixture(t)
	const sentinel = "wrap-sentinel"
	ring := uint64(types.SentinelAccuracyRingSize)

	recordUpheld(t, f, 2, 10, sentinel)      // epoch 2 -> slot 2
	recordUpheld(t, f, 2+ring, 11, sentinel) // same slot, overwrites epoch 2

	// At the wrapped epoch, only the new bump is in range; the stale epoch-2
	// entry must not be counted.
	up, ov, err := f.keeper.GetSentinelWindowedAccuracy(f.ctx, sentinel, 2+ring, ring)
	require.NoError(t, err)
	require.Equal(t, uint64(1), up)
	require.Equal(t, uint64(0), ov)
}

// TestAccuracyWindow_ZeroWindow returns no decided appeals for a zero window.
func TestAccuracyWindow_ZeroWindow(t *testing.T) {
	f := initFixture(t)
	const sentinel = "zero-window-sentinel"
	recordUpheld(t, f, 5, 10, sentinel)

	up, ov, err := f.keeper.GetSentinelWindowedAccuracy(f.ctx, sentinel, 5, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(0), up)
	require.Equal(t, uint64(0), ov)
}

// TestAccuracyWindow_MissingRecord returns zeros for an unknown sentinel.
func TestAccuracyWindow_MissingRecord(t *testing.T) {
	f := initFixture(t)
	up, ov, err := f.keeper.GetSentinelWindowedAccuracy(f.ctx, "no-such-sentinel", 10, 6)
	require.NoError(t, err)
	require.Equal(t, uint64(0), up)
	require.Equal(t, uint64(0), ov)
}

// TestAccuracyWindow_ResponsivenessVsLifetime is the motivating case: a sentinel
// with a strong lifetime record but a recent string of overturns reads as
// LOW accuracy through the window, where lifetime accuracy would still look
// high. Demonstrates the freshness the window buys.
func TestAccuracyWindow_ResponsivenessVsLifetime(t *testing.T) {
	f := initFixture(t)
	const sentinel = "veteran-sentinel"

	// Long history of upheld actions in old epochs (outside a 3-epoch window).
	for i := uint64(0); i < 20; i++ {
		recordUpheld(t, f, 1, 100+i, sentinel)
	}
	// Recent epochs 8,9,10: mostly overturned.
	recordOverturned(t, f, 8, 200, sentinel)
	recordOverturned(t, f, 9, 201, sentinel)
	recordUpheld(t, f, 10, 202, sentinel)

	// Lifetime would be 21 upheld / 23 decided ~= 0.91. The 3-epoch window
	// [8..10] is 1 upheld / 3 decided ~= 0.33 — correctly reflecting recent slip.
	up, ov, err := f.keeper.GetSentinelWindowedAccuracy(f.ctx, sentinel, 10, 3)
	require.NoError(t, err)
	require.Equal(t, uint64(1), up)
	require.Equal(t, uint64(2), ov)
}
