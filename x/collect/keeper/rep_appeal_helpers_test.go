package keeper_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	reptypes "sparkdream/x/rep/types"
)

// resolveAppealViaRep replays what x/rep's applyGovActionAppealVerdict does
// for a GOV_ACTION_TYPE_COLLECT_HIDE appeal: its own sentinel-bond and
// RoleActivity effects (against the rep mock), then the collect callbacks in
// the same order. overturned=true means the appellant wins.
func resolveAppealViaRep(t *testing.T, f *testFixture, hideRecordID uint64, overturned bool) {
	t.Helper()
	target := keeper.NewRepAppealTarget(f.keeper)
	at := reptypes.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE
	id := strconv.FormatUint(hideRecordID, 10)
	sentinel, err := target.GetActionSentinel(f.ctx, at, id)
	require.NoError(t, err)
	committed, err := target.GetActionCommittedAmount(f.ctx, at, id)
	require.NoError(t, err)
	rk := f.repKeeper
	if overturned {
		if sentinel != "" && committed.IsPositive() {
			require.NoError(t, rk.SlashBond(f.ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, sentinel, committed, "appeal_overturned"))
		}
		if sentinel != "" {
			require.NoError(t, rk.RecordRoleOutcome(f.ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, sentinel, reptypes.ActionKindCollectHide, false))
		}
		require.NoError(t, target.OnSentinelActionResolved(f.ctx, at, id))
		require.NoError(t, target.ReverseSentinelAction(f.ctx, at, id))
		require.NoError(t, target.OnAppealOutcome(f.ctx, at, id, reptypes.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED))
		return
	}
	if sentinel != "" && committed.IsPositive() {
		require.NoError(t, rk.ReleaseBond(f.ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, sentinel, committed))
	}
	if sentinel != "" {
		require.NoError(t, rk.RecordRoleOutcome(f.ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, sentinel, reptypes.ActionKindCollectHide, true))
	}
	require.NoError(t, target.OnSentinelActionResolved(f.ctx, at, id))
	require.NoError(t, target.OnAppealOutcome(f.ctx, at, id, reptypes.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD))
}

// timeoutAppealViaRep replays x/rep's TimeoutExpiredAppeals for a collect
// hide appeal: no sentinel effects in rep, only the outcome hook.
func timeoutAppealViaRep(t *testing.T, f *testFixture, hideRecordID uint64) {
	t.Helper()
	target := keeper.NewRepAppealTarget(f.keeper)
	require.NoError(t, target.OnAppealOutcome(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE,
		strconv.FormatUint(hideRecordID, 10), reptypes.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT))
}

// resolveAppealViaRepErr adapts resolveAppealViaRep to call sites written
// against the old error-returning ResolveHideAppeal callback.
func resolveAppealViaRepErr(t *testing.T, f *testFixture, hideRecordID uint64, overturned bool) error {
	t.Helper()
	resolveAppealViaRep(t, f, hideRecordID, overturned)
	return nil
}
