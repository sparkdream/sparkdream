package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// RoleActivity is the shared accountability record for bonded roles:
// per-kind action counters, verdict streaks, the overturn cooldown, the
// rolling accuracy ring, and streak demotion. Consolidated here from the
// forum-side tests that covered this machinery before the record moved
// to x/rep (see docs/x-rep-spec.md, RoleActivity).

const raRole = types.RoleType_ROLE_TYPE_CONTENT_SENTINEL

// raFixture returns a fixture with a 10-block reward-epoch cadence so tests
// can pick the outcome epoch via the block height (epoch = height / 10).
func raFixture(t *testing.T) *fixture {
	t.Helper()
	params := types.DefaultParams()
	params.SentinelRewardEpochBlocks = 10
	f := initFixture(t, WithCustomParams(params))
	f.ctx = f.ctx.WithBlockTime(time.Unix(1_000_000, 0))
	return f
}

func atEpoch(f *fixture, epoch uint64) sdk.Context {
	return f.ctx.WithBlockHeight(int64(epoch * 10))
}

func TestRoleActivity_ActionCountersAndReset(t *testing.T) {
	f := raFixture(t)
	addr := "activity-sentinel"

	require.NoError(t, f.keeper.RecordRoleAction(f.ctx, raRole, addr, types.ActionKindForumHide))
	require.NoError(t, f.keeper.RecordRoleAction(f.ctx, raRole, addr, types.ActionKindForumHide))
	require.NoError(t, f.keeper.RecordRoleAction(f.ctx, raRole, addr, types.ActionKindCollectHide))
	require.NoError(t, f.keeper.BumpRoleEpochAppealsResolved(f.ctx, raRole, addr))

	require.Equal(t, uint64(2), f.keeper.RoleEpochActionCount(f.ctx, raRole, addr, types.ActionKindForumHide))
	require.Equal(t, uint64(1), f.keeper.RoleEpochActionCount(f.ctx, raRole, addr, types.ActionKindCollectHide))

	ra, err := f.keeper.GetRoleActivity(f.ctx, raRole, addr)
	require.NoError(t, err)
	require.Equal(t, uint64(2), ra.TotalActions[types.ActionKindForumHide])
	require.Equal(t, uint64(1), ra.EpochAppealsResolved)

	// Reset zeros epoch state, keeps lifetime.
	require.NoError(t, f.keeper.ResetRoleEpochCounters(f.ctx, raRole, addr))
	require.Equal(t, uint64(0), f.keeper.RoleEpochActionCount(f.ctx, raRole, addr, types.ActionKindForumHide))
	ra, err = f.keeper.GetRoleActivity(f.ctx, raRole, addr)
	require.NoError(t, err)
	require.Equal(t, uint64(2), ra.TotalActions[types.ActionKindForumHide])
	require.Equal(t, uint64(0), ra.EpochAppealsResolved)
}

func TestRoleActivity_OutcomeStreaksCooldownAndDemotion(t *testing.T) {
	f := raFixture(t)
	addr := "outcome-sentinel"
	require.NoError(t, f.keeper.BondedRoles.Set(f.ctx, bondedRoleKey(raRole, addr), types.BondedRole{
		RoleType:    raRole,
		Address:     addr,
		CurrentBond: "1000000000",
		BondStatus:  types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
	}))

	// Upheld: streak up, no cooldown.
	require.NoError(t, f.keeper.RecordRoleOutcome(f.ctx, raRole, addr, types.ActionKindForumHide, true))
	ra, err := f.keeper.GetRoleActivity(f.ctx, raRole, addr)
	require.NoError(t, err)
	require.Equal(t, uint64(1), ra.ConsecutiveUpheld)
	require.Equal(t, uint64(1), ra.UpheldActions[types.ActionKindForumHide])
	require.Zero(t, f.keeper.RoleOverturnCooldownUntil(f.ctx, raRole, addr))

	// Overturned moderation action: streak flips, cooldown starts.
	require.NoError(t, f.keeper.RecordRoleOutcome(f.ctx, raRole, addr, types.ActionKindCollectHide, false))
	ra, err = f.keeper.GetRoleActivity(f.ctx, raRole, addr)
	require.NoError(t, err)
	require.Equal(t, uint64(1), ra.ConsecutiveOverturns)
	require.Equal(t, uint64(0), ra.ConsecutiveUpheld)
	require.Greater(t, f.keeper.RoleOverturnCooldownUntil(f.ctx, raRole, addr), int64(0))

	// Streak crossing the threshold demotes the bond — internal, no forum
	// callback needed. Mixed surfaces count toward ONE streak.
	for i := uint64(1); i < types.DefaultMaxConsecutiveOverturnsBeforeDemotion; i++ {
		kind := types.ActionKindForumLock
		if i%2 == 0 {
			kind = types.ActionKindCollectHide
		}
		require.NoError(t, f.keeper.RecordRoleOutcome(f.ctx, raRole, addr, kind, false))
	}
	br, err := f.keeper.GetBondedRole(f.ctx, raRole, addr)
	require.NoError(t, err)
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, br.BondStatus,
		"cross-surface overturn streak demotes the role")
}

func TestRoleActivity_CurationOverturnNoCooldown(t *testing.T) {
	f := raFixture(t)
	addr := "curation-sentinel"

	// A rejected curation proposal is an overturned verdict for streaks and
	// the ring, but must NOT start the moderation-action cooldown.
	require.NoError(t, f.keeper.RecordRoleOutcome(f.ctx, raRole, addr, types.ActionKindForumCuration, false))
	ra, err := f.keeper.GetRoleActivity(f.ctx, raRole, addr)
	require.NoError(t, err)
	require.Equal(t, uint64(1), ra.ConsecutiveOverturns)
	require.Zero(t, f.keeper.RoleOverturnCooldownUntil(f.ctx, raRole, addr),
		"curation rejections must not lock the sentinel out of moderation")
}

// --- Accuracy-ring semantics (ported from the forum-side window tests) ---

func recordOutcomeAt(t *testing.T, f *fixture, epoch uint64, addr string, upheld bool) {
	t.Helper()
	require.NoError(t, f.keeper.RecordRoleOutcome(atEpoch(f, epoch), raRole, addr, types.ActionKindForumHide, upheld))
}

// TestAccuracyWindow_RingSizeMatchesRepCap guards the invariant that the
// ring has at least as many slots as the largest window the reward
// distribution will ask for. If these drift, the ring could be asked to
// resolve a window longer than it can store.
func TestAccuracyWindow_RingSizeMatchesRepCap(t *testing.T) {
	require.Equal(t, uint64(types.RoleAccuracyRingSize), types.MaxSentinelAccuracyWindowEpochs)
}

// TestAccuracyWindow_RecordAndRead verifies upheld/overturned land in the
// correct epoch buckets and that the windowed read sums only the requested span.
func TestAccuracyWindow_RecordAndRead(t *testing.T) {
	f := raFixture(t)
	const addr = "window-sentinel"

	recordOutcomeAt(t, f, 5, addr, true)
	recordOutcomeAt(t, f, 5, addr, true)
	recordOutcomeAt(t, f, 6, addr, false)

	// Window covering epochs [1..6] sees everything.
	up, ov := f.keeper.GetRoleWindowedAccuracy(f.ctx, raRole, addr, 6, 6)
	require.Equal(t, uint64(2), up)
	require.Equal(t, uint64(1), ov)

	// Window of 1 ending at epoch 6 sees only epoch 6.
	up, ov = f.keeper.GetRoleWindowedAccuracy(f.ctx, raRole, addr, 6, 1)
	require.Equal(t, uint64(0), up)
	require.Equal(t, uint64(1), ov)
}

// TestAccuracyWindow_GapNoLeak verifies that an inactive epoch in the middle of
// the window does not leak older tallies into the current read.
func TestAccuracyWindow_GapNoLeak(t *testing.T) {
	f := raFixture(t)
	const addr = "gap-sentinel"

	recordOutcomeAt(t, f, 1, addr, true)  // epoch 1
	recordOutcomeAt(t, f, 3, addr, false) // epoch 3 (epoch 2 inactive)

	// Window of 2 ending at epoch 3 covers [2,3] -> epoch 1 excluded.
	up, ov := f.keeper.GetRoleWindowedAccuracy(f.ctx, raRole, addr, 3, 2)
	require.Equal(t, uint64(0), up)
	require.Equal(t, uint64(1), ov)
}

// TestAccuracyWindow_StaleSlotEvicted verifies that when an epoch wraps around
// to a slot already occupied by an out-of-window epoch (epoch and epoch+ringsize
// share a slot), the stale tally is dropped rather than counted.
func TestAccuracyWindow_StaleSlotEvicted(t *testing.T) {
	f := raFixture(t)
	const addr = "wrap-sentinel"
	ring := uint64(types.RoleAccuracyRingSize)

	recordOutcomeAt(t, f, 2, addr, true)      // epoch 2 -> slot 2
	recordOutcomeAt(t, f, 2+ring, addr, true) // same slot, overwrites epoch 2

	// At the wrapped epoch, only the new bump is in range; the stale epoch-2
	// entry must not be counted.
	up, ov := f.keeper.GetRoleWindowedAccuracy(f.ctx, raRole, addr, 2+ring, ring)
	require.Equal(t, uint64(1), up)
	require.Equal(t, uint64(0), ov)
}

// TestAccuracyWindow_ZeroWindow returns no decided appeals for a zero window.
func TestAccuracyWindow_ZeroWindow(t *testing.T) {
	f := raFixture(t)
	const addr = "zero-window-sentinel"
	recordOutcomeAt(t, f, 5, addr, true)

	up, ov := f.keeper.GetRoleWindowedAccuracy(f.ctx, raRole, addr, 5, 0)
	require.Equal(t, uint64(0), up)
	require.Equal(t, uint64(0), ov)
}

// TestAccuracyWindow_MissingRecord returns zeros for an unknown holder.
func TestAccuracyWindow_MissingRecord(t *testing.T) {
	f := raFixture(t)
	up, ov := f.keeper.GetRoleWindowedAccuracy(f.ctx, raRole, "no-such-sentinel", 10, 6)
	require.Equal(t, uint64(0), up)
	require.Equal(t, uint64(0), ov)
}

// TestAccuracyWindow_ResponsivenessVsLifetime is the motivating case: a holder
// with a strong lifetime record but a recent string of overturns reads as
// LOW accuracy through the window, where lifetime accuracy would still look
// high. Demonstrates the freshness the window buys.
func TestAccuracyWindow_ResponsivenessVsLifetime(t *testing.T) {
	f := raFixture(t)
	const addr = "veteran-sentinel"

	// Long history of upheld actions in an old epoch (outside a 3-epoch window).
	for i := 0; i < 20; i++ {
		recordOutcomeAt(t, f, 1, addr, true)
	}
	// Recent epochs 8,9,10: mostly overturned.
	recordOutcomeAt(t, f, 8, addr, false)
	recordOutcomeAt(t, f, 9, addr, false)
	recordOutcomeAt(t, f, 10, addr, true)

	// Lifetime would be 21 upheld / 23 decided ~= 0.91. The 3-epoch window
	// [8..10] is 1 upheld / 3 decided ~= 0.33 — correctly reflecting recent slip.
	up, ov := f.keeper.GetRoleWindowedAccuracy(f.ctx, raRole, addr, 10, 3)
	require.Equal(t, uint64(1), up)
	require.Equal(t, uint64(2), ov)
}
