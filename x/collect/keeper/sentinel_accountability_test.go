package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

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

func TestResolveHideAppeal_ReportsOutcomes(t *testing.T) {
	t.Run("overturned reported as sentinelUpheld=false", func(t *testing.T) {
		f := initTestFixture(t)
		denyCouncil(f)
		f.setBlockHeight(100)

		_, hrID, _ := setupHiddenCollectionWithPenalties(t, f)
		appealHide(t, f, hrID)
		require.NoError(t, f.keeper.ResolveHideAppeal(f.ctx, hrID, true)) // appeal upheld = sentinel wrong

		require.Equal(t, []roleOutcomeCall{{roleType: reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, addr: f.sentinel, kind: reptypes.ActionKindCollectHide, upheld: false}},
			f.repKeeper.roleOutcomeCalls)
		// The appeal filing itself was also counted (Gate 4 appeal rate).
		require.Contains(t, f.repKeeper.roleActionCalls,
			roleActionCall{addr: f.sentinel, kind: reptypes.ActionKindCollectAppealFiled})
	})

	t.Run("rejected reported as sentinelUpheld=true", func(t *testing.T) {
		f := initTestFixture(t)
		denyCouncil(f)
		f.setBlockHeight(100)

		_, hrID, _ := setupHiddenCollectionWithPenalties(t, f)
		appealHide(t, f, hrID)
		require.NoError(t, f.keeper.ResolveHideAppeal(f.ctx, hrID, false)) // appeal rejected = sentinel right

		require.Equal(t, []roleOutcomeCall{{roleType: reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, addr: f.sentinel, kind: reptypes.ActionKindCollectHide, upheld: true}},
			f.repKeeper.roleOutcomeCalls)
	})
}
