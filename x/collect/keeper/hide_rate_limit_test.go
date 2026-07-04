package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
)

// TestHideContent_DailyRateLimit pins the per-sentinel daily hide cap
// (max_hides_per_sentinel_per_day): hides past the cap are rejected within
// the same block-height day, a self-correct unhide does NOT refund the
// day's slot, and the counter resets on the next day.
func TestHideContent_DailyRateLimit(t *testing.T) {
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)

	params := types.DefaultParams()
	params.MaxHidesPerSentinelPerDay = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	hide := func(collID uint64) error {
		_, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
			Creator:    f.sentinel,
			TargetId:   collID,
			TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
		})
		return err
	}

	collA := f.createCollection(t, f.owner)
	collB := f.createCollection(t, f.owner)
	collC := f.createCollection(t, f.owner)

	// First hide + self-correct: the unhide must NOT refund the day's slot.
	require.NoError(t, hide(collA))
	hrID := uint64(0) // first record from a fresh fixture
	_, err := f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{
		Creator:      f.sentinel,
		HideRecordId: hrID,
	})
	require.NoError(t, err)

	// Second hide fills the cap; the third is rejected even though only two
	// hides are currently standing... in fact only one is (collA was
	// self-corrected) — the cap counts hide ACTIONS, not standing hides.
	require.NoError(t, hide(collB))
	err = hide(collC)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMaxDailyReactions)

	// Next block-height day: the counter starts fresh.
	f.advanceBlockHeight(14400) // BlocksPerDay
	require.NoError(t, hide(collC))
}
