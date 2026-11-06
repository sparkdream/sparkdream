package keeper_test

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

// COL-101 regression: an unappealed hide that expires releases its sentinel
// reservation exactly once. The target delete walks every hide record on the
// target, the expiring one included; before the fix the expiry handler then
// released again from its stale copy, which (rep saturating at zero) freed a
// second, still-open hide's reservation.
func TestUnappealedHideExpiryReleasesReservationOnce(t *testing.T) {
	for _, tc := range []struct {
		name       string
		targetType types.FlagTargetType
	}{
		{"collection", types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION},
		{"item", types.FlagTargetType_FLAG_TARGET_TYPE_ITEM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			committed := func() math.Int {
				br := f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)]
				v, ok := math.NewIntFromString(br.TotalCommittedBond)
				if !ok {
					return math.ZeroInt()
				}
				return v
			}
			hide := func(height int64) uint64 {
				f.setBlockHeight(height)
				collID := f.createCollection(t, f.owner)
				target := collID
				if tc.targetType == types.FlagTargetType_FLAG_TARGET_TYPE_ITEM {
					target = f.addItem(t, collID, f.owner)
				}
				resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
					Creator:    f.sentinel,
					TargetType: tc.targetType,
					TargetId:   target,
					ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
				})
				require.NoError(t, err)
				return resp.HideRecordId
			}

			base := committed()
			first := hide(100)
			second := hide(5000)
			hr1, err := f.keeper.HideRecord.Get(f.ctx, first)
			require.NoError(t, err)
			hr2, err := f.keeper.HideRecord.Get(f.ctx, second)
			require.NoError(t, err)
			require.True(t, hr1.CommittedAmount.IsPositive())
			require.True(t, committed().Equal(base.Add(hr1.CommittedAmount).Add(hr2.CommittedAmount)))

			// Only the first hide's deadline has passed.
			f.setBlockHeight(hr1.AppealDeadline + 1)
			require.NoError(t, f.keeper.PruneExpired(f.ctx))

			hr1, err = f.keeper.HideRecord.Get(f.ctx, first)
			require.NoError(t, err)
			require.True(t, hr1.Resolved)
			require.True(t, committed().Equal(base.Add(hr2.CommittedAmount)),
				"the open hide's reservation must survive: got %s, want %s", committed(), base.Add(hr2.CommittedAmount))
		})
	}
}

// COL-103 regression: deleting a collection while one of its items' hides is
// under appeal must not void the appeal. The record stays with x/rep, and the
// verdict still slashes the sentinel's reservation and records the overturn.
func TestCollectionDeleteKeepsItemHideAppealForTheVerdict(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createCollection(t, f.owner)
	itemID := f.addItem(t, collID, f.owner)
	resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_ITEM,
		TargetId:   itemID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	hrID := resp.HideRecordId
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	f.advanceBlockHeight(params.AppealCooldownBlocks + 1)
	_, err = f.msgServer.AppealHide(f.ctx, &types.MsgAppealHide{Creator: f.owner, HideRecordId: hrID})
	require.NoError(t, err)

	roleKey := mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)
	committedBefore := f.repKeeper.bondedRoles[roleKey].TotalCommittedBond
	bondBefore := f.repKeeper.bondedRoles[roleKey].CurrentBond

	_, err = f.msgServer.DeleteCollection(f.ctx, &types.MsgDeleteCollection{Creator: f.owner, Id: collID})
	require.NoError(t, err)

	hr, err := f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	require.False(t, hr.Resolved, "the appeal still awaits its verdict")
	require.Equal(t, committedBefore, f.repKeeper.bondedRoles[roleKey].TotalCommittedBond,
		"the reservation still backs the appeal")

	resolveAppealViaRep(t, f, hrID, true)

	hr, err = f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)
	require.NotEqual(t, bondBefore, f.repKeeper.bondedRoles[roleKey].CurrentBond, "the overturn slashed the sentinel")
	require.Len(t, f.repKeeper.roleOutcomeCalls, 1)
	require.False(t, f.repKeeper.roleOutcomeCalls[0].upheld)
}

// COL-105 regression: deleting an item drops its hide records' by-target
// index entries (the records stay), as deleting a collection does.
func TestItemDeleteDropsHideRecordIndex(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createCollection(t, f.owner)
	itemID := f.addItem(t, collID, f.owner)
	resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_ITEM,
		TargetId:   itemID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	hr, err := f.keeper.HideRecord.Get(f.ctx, resp.HideRecordId)
	require.NoError(t, err)

	f.setBlockHeight(hr.AppealDeadline + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	_, err = f.keeper.Item.Get(f.ctx, itemID)
	require.Error(t, err, "the unappealed hide deleted the item")

	has, err := f.keeper.HideRecordByTarget.Has(f.ctx, collections.Join(
		keeper.HideRecordTargetCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, itemID), resp.HideRecordId))
	require.NoError(t, err)
	require.False(t, has, "index entry dropped with the item")
	_, err = f.keeper.HideRecord.Get(f.ctx, resp.HideRecordId)
	require.NoError(t, err, "the record itself stays")
}

// COL-201 regression: when the unappealed-hide expiry has settled the record
// (bond released, resolved) but the collection delete fails, the record is
// re-indexed at the next block and the delete retried, so a permanent
// collection never stays HIDDEN with every exit closed. The bond is released
// exactly once across the attempts.
func TestUnappealedHideExpiryRetriesFailedDelete(t *testing.T) {
	f := initTestFixture(t)
	roleKey := mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)
	committed := func() string { return f.repKeeper.bondedRoles[roleKey].TotalCommittedBond }
	base := committed()

	f.setBlockHeight(100)
	// A TTL collection escrows a deposit the delete must refund; the TTL is
	// far enough out that only the hide expiry acts here.
	collID := f.createTTLCollection(t, f.owner, 1_000_000)
	resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	hr, err := f.keeper.HideRecord.Get(f.ctx, resp.HideRecordId)
	require.NoError(t, err)
	require.NotEqual(t, base, committed(), "the hide reserved bond")
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.True(t, coll.DepositAmount.IsPositive(), "the delete has a refund to fail on")

	// Every module payout fails: the atomic delete rolls back.
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(context.Context, string, sdk.AccAddress, sdk.Coins) error {
		return errors.New("module account short")
	}
	f.bankKeeper.burnCoinsFn = func(context.Context, string, sdk.Coins) error {
		return errors.New("module account short")
	}
	f.setBlockHeight(hr.AppealDeadline + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	coll, err = f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err, "the collection survives the failed delete")
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_HIDDEN, coll.Status)
	hr, err = f.keeper.HideRecord.Get(f.ctx, resp.HideRecordId)
	require.NoError(t, err)
	require.True(t, hr.Resolved, "the record is settled")
	require.Equal(t, base, committed(), "the reservation was released once")
	has, err := f.keeper.HideRecordExpiry.Has(f.ctx, collections.Join(hr.AppealDeadline+2, hr.Id))
	require.NoError(t, err)
	require.True(t, has, "the record is re-indexed at the next block")

	// Still failing: deferred again, bond untouched.
	f.setBlockHeight(hr.AppealDeadline + 2)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, base, committed(), "a retry never releases again")
	has, err = f.keeper.HideRecordExpiry.Has(f.ctx, collections.Join(hr.AppealDeadline+3, hr.Id))
	require.NoError(t, err)
	require.True(t, has)

	// Bank recovers: the retry deletes and the queue entry is gone.
	f.bankKeeper.sendCoinsFromModuleToAccountFn = nil
	f.bankKeeper.burnCoinsFn = nil
	f.setBlockHeight(hr.AppealDeadline + 3)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err, "the retry deleted the collection")
	require.Equal(t, base, committed())
	n := 0
	require.NoError(t, f.keeper.HideRecordExpiry.Walk(f.ctx, nil, func(collections.Pair[int64, uint64]) (bool, error) {
		n++
		return false, nil
	}))
	require.Zero(t, n, "no expiry entry left behind")
}
