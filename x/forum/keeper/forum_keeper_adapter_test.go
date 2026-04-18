package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// TestGetActionSentinel exercises all three action-type branches of the
// forum adapter's sentinel lookup.
func TestGetActionSentinel(t *testing.T) {
	f := initFixture(t)

	// Seed hide / lock / move records.
	require.NoError(t, f.keeper.HideRecord.Set(f.ctx, 1, types.HideRecord{
		PostId:   1,
		Sentinel: "sentinel-hide",
	}))
	require.NoError(t, f.keeper.ThreadLockRecord.Set(f.ctx, 2, types.ThreadLockRecord{
		RootId:   2,
		Sentinel: "sentinel-lock",
	}))
	require.NoError(t, f.keeper.ThreadMoveRecord.Set(f.ctx, 3, types.ThreadMoveRecord{
		RootId:   3,
		Sentinel: "sentinel-move",
	}))

	cases := []struct {
		name       string
		actionType reptypes.GovActionType
		target     string
		want       string
	}{
		{"hide-like", reptypes.GovActionType_GOV_ACTION_TYPE_WARNING, "1", "sentinel-hide"},
		{"thread lock", reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK, "2", "sentinel-lock"},
		{"thread move", reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE, "3", "sentinel-move"},
		{"missing record returns empty", reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK, "999", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.keeper.GetActionSentinel(f.ctx, tc.actionType, tc.target)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("unparseable target errors", func(t *testing.T) {
		_, err := f.keeper.GetActionSentinel(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK, "not-a-number")
		require.Error(t, err)
	})
}

// TestRecordSentinelActionUpheld exercises the counter increment logic for
// each action type and the streak-reset behaviour.
func TestRecordSentinelActionUpheld(t *testing.T) {
	f := initFixture(t)
	sentinel := "sentinel-upheld"

	require.NoError(t, f.keeper.HideRecord.Set(f.ctx, 10, types.HideRecord{PostId: 10, Sentinel: sentinel}))
	require.NoError(t, f.keeper.ThreadLockRecord.Set(f.ctx, 11, types.ThreadLockRecord{RootId: 11, Sentinel: sentinel}))
	require.NoError(t, f.keeper.ThreadMoveRecord.Set(f.ctx, 12, types.ThreadMoveRecord{RootId: 12, Sentinel: sentinel}))
	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, sentinel, types.SentinelActivity{
		Address:              sentinel,
		PendingHideCount:     5,
		ConsecutiveOverturns: 2,
	}))

	// Hide upheld: increments UpheldHides + decrements pending.
	require.NoError(t, f.keeper.RecordSentinelActionUpheld(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_WARNING, "10"))
	sa, err := f.keeper.SentinelActivity.Get(f.ctx, sentinel)
	require.NoError(t, err)
	require.Equal(t, uint64(1), sa.UpheldHides)
	require.Equal(t, uint64(4), sa.PendingHideCount)
	require.Equal(t, uint64(1), sa.ConsecutiveUpheld)
	require.Equal(t, uint64(0), sa.ConsecutiveOverturns)

	// Lock upheld.
	require.NoError(t, f.keeper.RecordSentinelActionUpheld(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK, "11"))
	sa, err = f.keeper.SentinelActivity.Get(f.ctx, sentinel)
	require.NoError(t, err)
	require.Equal(t, uint64(1), sa.UpheldLocks)
	require.Equal(t, uint64(2), sa.ConsecutiveUpheld)

	// Move upheld.
	require.NoError(t, f.keeper.RecordSentinelActionUpheld(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE, "12"))
	sa, err = f.keeper.SentinelActivity.Get(f.ctx, sentinel)
	require.NoError(t, err)
	require.Equal(t, uint64(1), sa.UpheldMoves)
	require.Equal(t, uint64(3), sa.ConsecutiveUpheld)
}

// TestGetSentinelActivityCounters_MissingReturnsZero verifies that the adapter
// returns a zero-valued snapshot (with no error) when forum has no record for
// the sentinel.
func TestGetSentinelActivityCounters_MissingReturnsZero(t *testing.T) {
	f := initFixture(t)

	got, err := f.keeper.GetSentinelActivityCounters(f.ctx, "no-such-sentinel")
	require.NoError(t, err)
	require.Equal(t, reptypes.SentinelActivityCounters{}, got)
}

// TestGetSentinelActivityCounters_MapsFields loads a fully-populated forum
// record and verifies every field the adapter exposes maps through cleanly.
func TestGetSentinelActivityCounters_MapsFields(t *testing.T) {
	f := initFixture(t)
	addr := "sentinel-counters"

	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, addr, types.SentinelActivity{
		Address:              addr,
		UpheldHides:          11,
		OverturnedHides:      2,
		UpheldLocks:          3,
		OverturnedLocks:      1,
		UpheldMoves:          4,
		OverturnedMoves:      0,
		EpochHides:           7,
		EpochLocks:           5,
		EpochMoves:           2,
		EpochPins:            1,
		EpochAppealsFiled:    3,
		EpochAppealsResolved: 2,
		// Cumulative-only fields (not exposed via adapter) should be ignored.
		TotalHides: 99,
	}))

	got, err := f.keeper.GetSentinelActivityCounters(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, reptypes.SentinelActivityCounters{
		UpheldHides:          11,
		OverturnedHides:      2,
		UpheldLocks:          3,
		OverturnedLocks:      1,
		UpheldMoves:          4,
		OverturnedMoves:      0,
		EpochHides:           7,
		EpochLocks:           5,
		EpochMoves:           2,
		EpochPins:            1,
		EpochAppealsFiled:    3,
		EpochAppealsResolved: 2,
	}, got)
}

// TestResetSentinelEpochCounters_ZerosEpochKeepsCumulative ensures the adapter
// wipes only the per-epoch counters and preserves cumulative / identity fields.
func TestResetSentinelEpochCounters_ZerosEpochKeepsCumulative(t *testing.T) {
	f := initFixture(t)
	addr := "sentinel-reset"

	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, addr, types.SentinelActivity{
		Address:              addr,
		UpheldHides:          11,
		OverturnedHides:      2,
		TotalHides:           99,
		PendingHideCount:     4,
		ConsecutiveUpheld:    3,
		ConsecutiveOverturns: 0,
		EpochHides:           7,
		EpochLocks:           5,
		EpochMoves:           2,
		EpochPins:            1,
		EpochAppealsFiled:    3,
		EpochAppealsResolved: 2,
	}))

	require.NoError(t, f.keeper.ResetSentinelEpochCounters(f.ctx, addr))

	sa, err := f.keeper.SentinelActivity.Get(f.ctx, addr)
	require.NoError(t, err)
	require.Equal(t, uint64(0), sa.EpochHides)
	require.Equal(t, uint64(0), sa.EpochLocks)
	require.Equal(t, uint64(0), sa.EpochMoves)
	require.Equal(t, uint64(0), sa.EpochPins)
	require.Equal(t, uint64(0), sa.EpochAppealsFiled)
	require.Equal(t, uint64(0), sa.EpochAppealsResolved)
	// Cumulative / identity preserved.
	require.Equal(t, uint64(11), sa.UpheldHides)
	require.Equal(t, uint64(2), sa.OverturnedHides)
	require.Equal(t, uint64(99), sa.TotalHides)
	require.Equal(t, uint64(4), sa.PendingHideCount)
	require.Equal(t, uint64(3), sa.ConsecutiveUpheld)
	require.Equal(t, addr, sa.Address)
}

// TestResetSentinelEpochCounters_MissingNoOp verifies that calling reset on a
// non-existent sentinel is a no-op (no error, nothing written).
func TestResetSentinelEpochCounters_MissingNoOp(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.ResetSentinelEpochCounters(f.ctx, "does-not-exist"))
	// Nothing should be persisted; a subsequent Get should still error.
	_, err := f.keeper.SentinelActivity.Get(f.ctx, "does-not-exist")
	require.Error(t, err)
}

// TestRecordSentinelActionOverturnedTriggersDemotion verifies that a streak
// of consecutive overturns past the threshold calls the rep keeper to demote
// the sentinel.
func TestRecordSentinelActionOverturnedTriggersDemotion(t *testing.T) {
	f := initFixture(t)
	sentinel := "sentinel-overturned"

	// Seed records for 3 different hides so three consecutive overturns can
	// be recorded.
	for i := uint64(100); i < 103; i++ {
		require.NoError(t, f.keeper.HideRecord.Set(f.ctx, i, types.HideRecord{PostId: i, Sentinel: sentinel}))
	}
	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, sentinel, types.SentinelActivity{
		Address:           sentinel,
		PendingHideCount:  3,
		ConsecutiveUpheld: 5,
	}))
	// Register sentinel on rep mock so SetBondStatus can find it.
	if f.repKeeper.sentinels == nil {
		f.repKeeper.sentinels = make(map[string]reptypes.SentinelActivity)
	}
	f.repKeeper.sentinels[sentinel] = reptypes.SentinelActivity{
		Address:    sentinel,
		BondStatus: reptypes.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
	}

	for i := uint64(100); i < 103; i++ {
		require.NoError(t, f.keeper.RecordSentinelActionOverturned(
			f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_WARNING, fmt.Sprintf("%d", i),
		))
	}

	sa, err := f.keeper.SentinelActivity.Get(f.ctx, sentinel)
	require.NoError(t, err)
	require.Equal(t, uint64(3), sa.OverturnedHides)
	require.Equal(t, uint64(3), sa.ConsecutiveOverturns)
	require.Equal(t, uint64(0), sa.ConsecutiveUpheld)

	// Demotion was applied via rep keeper.
	repSa, ok := f.repKeeper.sentinels[sentinel]
	require.True(t, ok)
	require.Equal(t, reptypes.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED, repSa.BondStatus)
	require.Greater(t, repSa.DemotionCooldownUntil, int64(0))
}
