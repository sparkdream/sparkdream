package keeper_test

import (
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

// TestOnSentinelActionResolved covers forum's remaining resolution
// bookkeeping: the pending-hide decrement for hide verdicts. All shared
// accountability (verdict counters, streaks, ring, cooldown, demotion)
// moved to x/rep's RoleActivity — see x/rep/keeper/role_activity_test.go.
func TestOnSentinelActionResolved(t *testing.T) {
	f := initFixture(t)

	// Seed a hide record + a pending count of 2.
	require.NoError(t, f.keeper.HideRecord.Set(f.ctx, 10, types.HideRecord{
		PostId:   10,
		Sentinel: testSentinel,
	}))
	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, testSentinel, types.SentinelActivity{
		Address:          testSentinel,
		PendingHideCount: 2,
	}))

	// Hide verdict -> pending decremented.
	require.NoError(t, f.keeper.OnSentinelActionResolved(f.ctx,
		reptypes.GovActionType_GOV_ACTION_TYPE_WARNING, "10")) // default branch = post hide
	act, err := f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
	require.NoError(t, err)
	require.Equal(t, uint64(1), act.PendingHideCount)

	// Lock/move/pin verdicts -> no-op.
	require.NoError(t, f.keeper.OnSentinelActionResolved(f.ctx,
		reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK, "10"))
	act, err = f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
	require.NoError(t, err)
	require.Equal(t, uint64(1), act.PendingHideCount)

	// Missing record -> soft no-op.
	require.NoError(t, f.keeper.OnSentinelActionResolved(f.ctx,
		reptypes.GovActionType_GOV_ACTION_TYPE_WARNING, "999"))
}
