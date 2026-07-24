package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

// denyCouncil pins the commons mock to reject council authorization.
// mockCommonsKeeper defaults IsCouncilAuthorized to TRUE (op-params tests
// rely on that), which would otherwise route moderation calls through the
// council path and defeat sentinel-path assertions (bond retention,
// window, authorization negatives).
func denyCouncil(f *testFixture) {
	f.commonsKeeper.isCouncilAuthorizedFn = func(_ context.Context, _, _, _ string) bool {
		return false
	}
}

// hideCollectionForUnhide hides the given collection as f.sentinel and
// returns the hide record ID.
func hideCollectionForUnhide(t *testing.T, f *testFixture, collID uint64) uint64 {
	t.Helper()
	resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetId:   collID,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
		ReasonText: "spam content",
	})
	require.NoError(t, err)
	return resp.HideRecordId
}

func sentinelCommitted(f *testFixture) string {
	return f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)].TotalCommittedBond
}

func TestUnhideContent_CollectionHappyPath(t *testing.T) {
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)

	// Collection with tags so the hide applies (and snapshots) the per-tag
	// rep penalty, plus a simulated author bond so the hide snapshots its
	// amount for restore.
	createResp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "phoenix-collection",
		Tags:       []string{"phoenix", "aurora"},
	})
	require.NoError(t, err)
	collID := createResp.Id

	bondAmount := math.NewInt(50_000_000)
	f.repKeeper.getAuthorBondFn = func(_ context.Context, targetType reptypes.StakeTargetType, targetID uint64) (reptypes.Stake, error) {
		require.Equal(t, reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND, targetType)
		require.Equal(t, collID, targetID)
		return reptypes.Stake{Amount: bondAmount}, nil
	}

	// Owner has plenty of rep on "phoenix" (full penalty deducted) but only
	// 7.5 on "aurora" (deduction floored — only 7.5 actually taken). The
	// restore must mirror the ACTUAL amounts, not the penalty param.
	f.repKeeper.getReputationScoresFn = func(_ context.Context, addr string) (map[string]string, error) {
		require.Equal(t, f.owner, addr)
		return map[string]string{"phoenix": "100.0", "aurora": "7.5"}, nil
	}

	preHideCommitted := sentinelCommitted(f)
	hrID := hideCollectionForUnhide(t, f, collID)
	postHideCommitted := sentinelCommitted(f)
	require.NotEqual(t, preHideCommitted, postHideCommitted)

	// Snapshots captured at hide time.
	hr, err := f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	require.Equal(t, bondAmount, hr.AuthorBondAmount)
	require.Equal(t, types.DefaultAuthorRepPenalty, hr.AuthorRepPenalty)
	require.Equal(t, []string{"phoenix", "aurora"}, hr.RepPenaltyTags)
	require.Equal(t, []string{
		types.DefaultAuthorRepPenalty.String(), // min(100, 15) = 15
		math.LegacyMustNewDecFromStr("7.5").String(),
	}, hr.RepPenaltyAmounts)
	deadline := hr.AppealDeadline

	// Self-correct within the window.
	f.setBlockHeight(200)
	_, err = f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{
		Creator:      f.sentinel,
		HideRecordId: hrID,
	})
	require.NoError(t, err)

	// Collection restored to ACTIVE, status index moved.
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
	activeKey := collections.Join3(int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE), int32(1), collID)
	hiddenKey := collections.Join3(int32(types.CollectionStatus_COLLECTION_STATUS_HIDDEN), int32(1), collID)
	hasActive, err := f.keeper.CollectionsByStatus.Has(f.ctx, activeKey)
	require.NoError(t, err)
	require.True(t, hasActive)
	hasHidden, err := f.keeper.CollectionsByStatus.Has(f.ctx, hiddenKey)
	require.NoError(t, err)
	require.False(t, hasHidden)

	// Record closed as self-corrected.
	hr, err = f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)
	require.True(t, hr.SelfCorrected)

	// Author bond restored with the snapshotted amount.
	require.Len(t, f.repKeeper.restoreAuthorBondCalls, 1)
	require.Equal(t, bondAmount, f.repKeeper.restoreAuthorBondCalls[0].amount)
	require.Equal(t, collID, f.repKeeper.restoreAuthorBondCalls[0].targetID)
	require.Equal(t, f.ownerAddr, f.repKeeper.restoreAuthorBondCalls[0].author)

	// Rep penalty restored per tag with the snapshotted ACTUAL amounts.
	require.Len(t, f.repKeeper.addReputationCalls, 2)
	require.Equal(t, "phoenix", f.repKeeper.addReputationCalls[0].tag)
	require.Equal(t, types.DefaultAuthorRepPenalty, f.repKeeper.addReputationCalls[0].amount)
	require.Equal(t, "aurora", f.repKeeper.addReputationCalls[1].tag)
	require.Equal(t, math.LegacyMustNewDecFromStr("7.5"), f.repKeeper.addReputationCalls[1].amount)
	for _, call := range f.repKeeper.addReputationCalls {
		require.Equal(t, f.ownerAddr, call.addr)
	}

	// Anti-cycling: the sentinel's committed bond stays reserved and the
	// expiry entry is retained until the original deadline.
	require.Equal(t, postHideCommitted, sentinelCommitted(f))
	hasExpiry, err := f.keeper.HideRecordExpiry.Has(f.ctx, collections.Join(deadline, hrID))
	require.NoError(t, err)
	require.True(t, hasExpiry)

	// At the original deadline the EndBlocker releases the bond, removes
	// the expiry entry, and does NOT delete the (now ACTIVE) collection.
	f.setBlockHeight(deadline + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	require.Equal(t, preHideCommitted, sentinelCommitted(f))
	hasExpiry, err = f.keeper.HideRecordExpiry.Has(f.ctx, collections.Join(deadline, hrID))
	require.NoError(t, err)
	require.False(t, hasExpiry)
	coll, err = f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)

	// Idempotent: a later prune pass must not release the bond again.
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	require.Equal(t, preHideCommitted, sentinelCommitted(f))
}

func TestUnhideContent_Item(t *testing.T) {
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	itemID := f.addItem(t, collID, f.owner)

	resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetId:   itemID,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_ITEM,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)

	item, err := f.keeper.Item.Get(f.ctx, itemID)
	require.NoError(t, err)
	require.Equal(t, types.ItemStatus_ITEM_STATUS_HIDDEN, item.Status)

	_, err = f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{
		Creator:      f.sentinel,
		HideRecordId: resp.HideRecordId,
	})
	require.NoError(t, err)

	item, err = f.keeper.Item.Get(f.ctx, itemID)
	require.NoError(t, err)
	require.Equal(t, types.ItemStatus_ITEM_STATUS_ACTIVE, item.Status)

	// Items carry no author bond or rep penalty — nothing restored.
	require.Empty(t, f.repKeeper.restoreAuthorBondCalls)
	require.Empty(t, f.repKeeper.addReputationCalls)
}

func TestUnhideContent_Guards(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, f *testFixture, hrID uint64) (creator string, id uint64)
		expErrContains string
	}{
		{
			name: "record not found",
			setup: func(t *testing.T, f *testFixture, hrID uint64) (string, uint64) {
				return f.sentinel, hrID + 999
			},
			expErrContains: "hide record not found",
		},
		{
			name: "non-sentinel creator (owner) unauthorized",
			setup: func(t *testing.T, f *testFixture, hrID uint64) (string, uint64) {
				return f.owner, hrID
			},
			expErrContains: "only the sentinel who hid the content",
		},
		{
			name: "appealed hide cannot be self-corrected",
			setup: func(t *testing.T, f *testFixture, hrID uint64) (string, uint64) {
				// Appeals must wait appeal_cooldown_blocks after the hide.
				f.advanceBlockHeight(types.DefaultAppealCooldownBlocks + 1)
				_, err := f.msgServer.AppealHide(f.ctx, &types.MsgAppealHide{
					Creator:      f.owner,
					HideRecordId: hrID,
				})
				require.NoError(t, err)
				return f.sentinel, hrID
			},
			expErrContains: "hide has been appealed",
		},
		{
			name: "window expired",
			setup: func(t *testing.T, f *testFixture, hrID uint64) (string, uint64) {
				f.advanceBlockHeight(types.DefaultSentinelUnhideWindowBlocks + 1)
				return f.sentinel, hrID
			},
			expErrContains: "sentinel unhide window",
		},
		{
			name: "already resolved (double unhide)",
			setup: func(t *testing.T, f *testFixture, hrID uint64) (string, uint64) {
				_, err := f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{
					Creator:      f.sentinel,
					HideRecordId: hrID,
				})
				require.NoError(t, err)
				return f.sentinel, hrID
			},
			expErrContains: "hide record already resolved",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			denyCouncil(f)
			denyCouncil(f)
			f.setBlockHeight(100)
			collID := f.createCollection(t, f.owner)
			hrID := hideCollectionForUnhide(t, f, collID)

			creator, id := tc.setup(t, f, hrID)
			_, err := f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{
				Creator:      creator,
				HideRecordId: id,
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.expErrContains)
		})
	}
}

func TestUnhideContent_WindowBoundary(t *testing.T) {
	// Exactly at the window edge (delta == window) is still allowed;
	// window+1 is rejected (covered in TestUnhideContent_Guards).
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)
	collID := f.createCollection(t, f.owner)
	hrID := hideCollectionForUnhide(t, f, collID)

	f.advanceBlockHeight(types.DefaultSentinelUnhideWindowBlocks)
	_, err := f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{
		Creator:      f.sentinel,
		HideRecordId: hrID,
	})
	require.NoError(t, err)
}

func TestUnhideContent_ReHideReservesSecondCommit(t *testing.T) {
	// A re-hide after a self-correct is legitimate, but it must reserve a
	// second sentinel_commit_amount while the first is still locked —
	// hide/unhide cycling is not free.
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)
	collID := f.createCollection(t, f.owner)

	preCommitted, _ := math.NewIntFromString(sentinelCommitted(f))
	if preCommitted.IsNil() {
		preCommitted = math.ZeroInt()
	}

	hrID := hideCollectionForUnhide(t, f, collID)
	_, err := f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{
		Creator:      f.sentinel,
		HideRecordId: hrID,
	})
	require.NoError(t, err)

	// Re-hide passes the unresolved-record check (first record is Resolved).
	hideCollectionForUnhide(t, f, collID)

	postCommitted, _ := math.NewIntFromString(sentinelCommitted(f))
	expected := preCommitted.Add(types.DefaultSentinelCommitAmount.MulRaw(2))
	require.Equal(t, expected, postCommitted,
		"re-hide after self-correct must hold two commit reservations")
}
