package keeper_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

// Collect reports moderation actions, appeal filings, and jury outcomes
// into rep's shared RoleActivity record, and consumes the shared overturn
// cooldown (see docs/x-rep-spec.md, RoleActivity).

func TestHideContent_RecordsCollectActivity(t *testing.T) {
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	hideCollectionForUnhide(t, f, collID)

	require.Equal(t, []roleActionCall{{addr: f.sentinel, kind: reptypes.ActionKindCollectHide}},
		f.repKeeper.roleActionCalls,
		"sentinel-path hide must credit the shared activity record")
}

func TestHideContent_CouncilPathRecordsNothing(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	councilOnly(f, f.member)

	collID := f.createCollection(t, f.owner)
	_, err := hideWithAuthority(f, f.member, collID, types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL)
	require.NoError(t, err)

	require.Empty(t, f.repKeeper.roleActionCalls,
		"council hides carry no sentinel accountability")
}

func TestHideContent_SharedOverturnCooldownBlocks(t *testing.T) {
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)

	f.repKeeper.overturnCooldownUntil = map[string]int64{
		f.sentinel: f.sdkCtx.BlockTime().Unix() + 3600,
	}

	collID := f.createCollection(t, f.owner)
	_, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetId:   collID,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrSentinelCooldown)
}

// Under the x/rep appeal path, the sentinel verdict (RecordRoleOutcome) and
// the bond release/slash are applied by x/rep itself; collect's callbacks
// must not report them a second time. Collect still counts the appeal filing
// (Gate 4 appeal rate).
func TestRepAppealCallbacks_DoNotDoubleReportOutcomes(t *testing.T) {
	for _, outcome := range []reptypes.GovAppealStatus{
		reptypes.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED,
		reptypes.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD,
	} {
		t.Run(outcome.String(), func(t *testing.T) {
			f := initTestFixture(t)
			denyCouncil(f)
			f.setBlockHeight(100)

			_, hrID, _ := setupHiddenCollectionWithPenalties(t, f)
			appealHide(t, f, hrID)
			require.Contains(t, f.repKeeper.roleActionCalls,
				roleActionCall{addr: f.sentinel, kind: reptypes.ActionKindCollectAppealFiled})

			target := keeper.NewRepAppealTarget(f.keeper)
			id := strconv.FormatUint(hrID, 10)
			at := reptypes.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE
			if outcome == reptypes.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED {
				require.NoError(t, target.ReverseSentinelAction(f.ctx, at, id))
			}
			require.NoError(t, target.OnAppealOutcome(f.ctx, at, id, outcome))
			require.Empty(t, f.repKeeper.roleOutcomeCalls)
		})
	}
}
