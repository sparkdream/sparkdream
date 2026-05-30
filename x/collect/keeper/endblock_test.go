package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

func TestPruneExpiredCollections(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100 so expiry math works
	f.setBlockHeight(100)

	// Create a TTL collection that expires at block 200
	collID := f.createTTLCollection(t, f.owner, 200)

	// Verify it exists
	_, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)

	// Track deposit refund
	var refundCalled bool
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
		refundCalled = true
		return nil
	}

	// Advance past expiry
	f.setBlockHeight(201)

	// Run PruneExpired
	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify collection is deleted
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err)

	// Verify deposit refund was called
	require.True(t, refundCalled)
}

// Regression: the EndBlocker prune path routes through deleteCollectionFull,
// so it must also decrement UsageCount for every tag on the collection. Same
// contract as the user-driven MsgDeleteCollection — see
// TestDeleteCollectionDecrementsTagUsage for the direct-call variant.
func TestPruneExpiredCollections_DecrementsTagUsage(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// TTL collection with two tags. Expiry is at block 200.
	resp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "ttl-tagged",
		ExpiresAt:  200,
		Tags:       []string{"x", "y"},
	})
	require.NoError(t, err)
	require.Len(t, f.repKeeper.incrementTagUsageCalls, 2)
	f.repKeeper.decrementTagUsageCalls = nil

	// Advance past expiry and prune.
	f.setBlockHeight(201)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	// Collection gone.
	_, err = f.keeper.Collection.Get(f.ctx, resp.Id)
	require.Error(t, err)

	// Each tag decremented exactly once on the prune path.
	require.ElementsMatch(t, []string{"x", "y"}, f.repKeeper.decrementTagUsageCalls,
		"EndBlocker TTL prune must decrement every tag on the collection")
}

func TestPruneSponsorshipRequests(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create a TTL collection as nonMember (creates PENDING collection)
	collID := f.createPendingCollection(t)

	// Request sponsorship
	_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{
		Creator:      f.nonMember,
		CollectionId: collID,
	})
	require.NoError(t, err)

	// Verify sponsorship request exists
	_, err = f.keeper.SponsorshipRequest.Get(f.ctx, collID)
	require.NoError(t, err)

	// Track refund
	var refundCount int
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
		refundCount++
		return nil
	}

	// Advance past sponsorship request TTL (default 100800 blocks)
	// The request was created at block 100, expires at 100 + 100800 = 100900
	f.setBlockHeight(100901)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify sponsorship request is deleted
	_, err = f.keeper.SponsorshipRequest.Get(f.ctx, collID)
	require.Error(t, err)

	// Verify refund was called (deposits refunded)
	require.Greater(t, refundCount, 0)
}

func TestPruneUnappealedHides(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create an ACTIVE collection
	collID := f.createCollection(t, f.owner)

	// Hide it (sentinel)
	resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	hideRecordID := resp.HideRecordId

	// Verify collection is HIDDEN
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_HIDDEN, coll.Status)

	// Capture pre-prune commitment so we can verify ReleaseBond fires.
	preCommitted := f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, f.sentinel)].TotalCommittedBond

	// Advance past hide_expiry_blocks (default 100800)
	// Hidden at block 100, appeal deadline = 100 + 100800 = 100900
	f.setBlockHeight(100901)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify hide record is resolved
	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)

	// Verify collection is deleted (unappealed hide = content deleted)
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err)

	// Verify sentinel bond was released (TotalCommittedBond decreased).
	postCommitted := f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, f.sentinel)].TotalCommittedBond
	require.NotEqual(t, preCommitted, postCommitted, "expected ReleaseBond to reduce total_committed_bond")
}

// setupEndorsedHiddenCollection creates a PENDING non-member collection with
// the given TTL, has f.member endorse it (locking endorsement_dream_stake),
// then has f.sentinel hide it. Returns the collection ID, hide record ID, and
// the locked stake amount so tests can assert post-prune state. Installs
// recorders on the endorser's DREAM movements so callers can distinguish a
// BurnDREAM (slash) from an UnlockDREAM (refund).
func setupEndorsedHiddenCollection(t *testing.T, f *testFixture, ttlBlocksFromNow int64) (collID, hideRecordID uint64, stake math.Int) {
	t.Helper()

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	expiresAt := sdkCtx.BlockHeight() + ttlBlocksFromNow

	resp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.nonMember,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "endorsed-hidden",
		ExpiresAt:  expiresAt,
	})
	require.NoError(t, err)
	collID = resp.Id

	_, err = f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
		Creator:      f.nonMember,
		CollectionId: collID,
		Seeking:      true,
	})
	require.NoError(t, err)

	_, err = f.msgServer.EndorseCollection(f.ctx, &types.MsgEndorseCollection{
		Creator:      f.member,
		CollectionId: collID,
	})
	require.NoError(t, err)

	endorsement, err := f.keeper.Endorsement.Get(f.ctx, collID)
	require.NoError(t, err)
	require.False(t, endorsement.StakeReleased)
	stake = endorsement.DreamStake

	// Recorders for the endorser only — keeps the assertion specific to the
	// member's stake and ignores any unrelated DREAM movement.
	f.repKeeper.burnDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
		if addr.Equals(f.memberAddr) {
			f.repKeeper.burnCalls = append(f.repKeeper.burnCalls, dreamCall{addr: addr, amount: amount})
		}
		return nil
	}
	f.repKeeper.unlockDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
		if addr.Equals(f.memberAddr) {
			f.repKeeper.unlockCalls = append(f.repKeeper.unlockCalls, dreamCall{addr: addr, amount: amount})
		}
		return nil
	}

	hideResp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	hideRecordID = hideResp.HideRecordId

	return collID, hideRecordID, stake
}

func assertEndorsementSlashed(t *testing.T, f *testFixture, stake math.Int) {
	t.Helper()
	// Slash decrements the locked-stake tracker via UnlockDREAM (so
	// staked_dream returns to its pre-endorse level) AND burns from balance
	// via BurnDREAM. UnlockDREAM never refunds — it only moves between
	// internal trackers — so paired with BurnDREAM the endorser still
	// loses the full stake from their balance.
	require.Len(t, f.repKeeper.unlockCalls, 1, "expected one UnlockDREAM call on endorser before burn")
	require.True(t, f.repKeeper.unlockCalls[0].addr.Equals(f.memberAddr))
	require.Equal(t, stake.String(), f.repKeeper.unlockCalls[0].amount.String())

	require.Len(t, f.repKeeper.burnCalls, 1, "expected one BurnDREAM call on endorser")
	require.True(t, f.repKeeper.burnCalls[0].addr.Equals(f.memberAddr))
	require.Equal(t, stake.String(), f.repKeeper.burnCalls[0].amount.String())

	var found bool
	for _, ev := range sdk.UnwrapSDKContext(f.ctx).EventManager().Events() {
		if ev.Type == "endorsement_stake_slashed" {
			found = true
			break
		}
	}
	require.True(t, found, "expected endorsement_stake_slashed event")
}

// Regression: the unappealed-hide-expiry path (§10.3) on an endorsed
// non-member collection must burn the endorser's locked DREAM, not refund it.
// Pre-fix, deleteCollectionFull's blanket unlock-on-endorsed-delete path
// refunded the endorser whenever the owner declined to appeal — leaving zero
// economic deterrent on the realistic branch (rational bad-faith owners never
// appeal a justified hide). TTL is set far past the hide deadline here so the
// §10.3 walk wins the race against §10.1.
func TestPruneUnappealedHides_EndorsedCollection_SlashesEndorser(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// TTL well past hide_expiry (default 100800) so §10.1 doesn't fire first,
	// but under max_non_member_ttl (default 432000) so CreateCollection accepts.
	_, hideRecordID, stake := setupEndorsedHiddenCollection(t, f, 400_000)

	// Owner does NOT appeal. Advance past hide_expiry deadline = 100 + 100800
	// = 100900 (TTL is at 500100, still in the future).
	f.setBlockHeight(100901)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)

	assertEndorsementSlashed(t, f, stake)
}

// Regression: a TTL expiry (§10.1) that fires on an already-HIDDEN endorsed
// collection must also slash. Without this branch a spammer with a short-TTL
// collection sidesteps the §10.3 slash entirely — §10.1 runs first in
// PruneExpired and the TTL delete would otherwise refund the endorser. The
// slash decision lives inside deleteCollectionFull so every delete path that
// can race here closes the same loophole.
func TestPruneExpiredCollections_HiddenEndorsed_SlashesEndorser(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// TTL = 100200, well BEFORE hide_expiry (100 + 100800 = 100900).
	_, hideRecordID, stake := setupEndorsedHiddenCollection(t, f, 100)

	// Advance past TTL but BEFORE hide_expiry. §10.1 walks first and finds
	// the collection's TTL has passed; the hide deadline hasn't, so §10.3
	// wouldn't slash on its own pass.
	f.setBlockHeight(100201)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	// Collection is gone via the TTL path.
	_, err := f.keeper.Collection.Get(f.ctx, collIDOf(t, f, hideRecordID))
	require.Error(t, err)

	assertEndorsementSlashed(t, f, stake)
}

// collIDOf is a tiny helper: looks up the collection id off a hide record. The
// TTL-path test deletes the collection before we can re-fetch it, so we read
// the id off the HideRecord (which sticks around as Resolved=true).
func collIDOf(t *testing.T, f *testFixture, hideRecordID uint64) uint64 {
	t.Helper()
	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	return hr.TargetId
}

// setupEndorsedHiddenAppealedCollection extends setupEndorsedHiddenCollection
// by also filing the owner's appeal once the cooldown has elapsed. Returns
// the same triple. Block height afterward is `appealAt` so the test knows
// where the appeal_deadline starts counting from.
func setupEndorsedHiddenAppealedCollection(t *testing.T, f *testFixture, ttlBlocksFromNow int64, appealAt int64) (collID, hideRecordID uint64, stake math.Int) {
	t.Helper()
	collID, hideRecordID, stake = setupEndorsedHiddenCollection(t, f, ttlBlocksFromNow)

	f.setBlockHeight(appealAt)
	_, err := f.msgServer.AppealHide(f.ctx, &types.MsgAppealHide{
		Creator:      f.nonMember,
		HideRecordId: hideRecordID,
	})
	require.NoError(t, err)
	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Appealed)
	require.False(t, hr.Resolved)

	return collID, hideRecordID, stake
}

// Regression refinement (deferral): when an appeal is in flight and the
// jury has not yet ruled, the §10.1 TTL prune must NOT delete the
// collection. Doing so would either over-slash the endorser (if the jury
// would have upheld the appeal) or under-slash (if the deferred
// implementation suppressed the slash). Defer the deletion instead — the
// collection lingers past expires_at, and a later §10.1 pass deletes it
// after the appeal resolves (status restored to ACTIVE → unlock, or
// callbacks.go path → burn).
func TestPruneExpiredCollections_HiddenEndorsed_AppealPending_DefersDeletion(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// TTL = 1100. Owner appeals at block 701 (past AppealCooldownBlocks=600).
	// Default AppealDeadlineBlocks=201600, so the appeal deadline lands well
	// past the TTL we'll trigger.
	collID, _, stake := setupEndorsedHiddenAppealedCollection(t, f, 1000, 701)

	// Advance past TTL (1100). §10.1 should walk, see the in-flight appeal,
	// and skip — leaving the collection in place.
	f.setBlockHeight(1101)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	// Collection MUST still exist (deferred).
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err, "collection must NOT be deleted while appeal is in flight")
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_HIDDEN, coll.Status)

	// Endorser stake is still locked — nothing burned, nothing unlocked yet.
	require.Empty(t, f.repKeeper.burnCalls, "endorser DREAM must NOT be burned while appeal is in flight")
	require.Empty(t, f.repKeeper.unlockCalls, "endorser DREAM must NOT be unlocked while appeal is in flight")
	endorsement, err := f.keeper.Endorsement.Get(f.ctx, collID)
	require.NoError(t, err)
	require.False(t, endorsement.StakeReleased)
	require.Equal(t, stake.String(), endorsement.DreamStake.String())

	// Deferral event was emitted.
	var deferred bool
	for _, ev := range sdk.UnwrapSDKContext(f.ctx).EventManager().Events() {
		if ev.Type == "collection_expiry_deferred" {
			deferred = true
			break
		}
	}
	require.True(t, deferred, "expected collection_expiry_deferred event")
}

// Deferral resolves: appeal UPHELD (sentinel wrong) → status restored to
// ACTIVE → next §10.1 pass deletes via the normal unlock path. Endorser was
// vouching for content the jury agreed was legitimate, so they keep their
// stake.
func TestPruneExpiredCollections_HiddenEndorsed_AppealUpheld_Unlocks(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID, hideRecordID, stake := setupEndorsedHiddenAppealedCollection(t, f, 1000, 701)

	// Past TTL → first prune defers.
	f.setBlockHeight(1101)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	_, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)

	// Jury upholds — sentinel was wrong. ResolveHideAppeal restores status
	// to ACTIVE without deleting.
	require.NoError(t, f.keeper.ResolveHideAppeal(f.ctx, hideRecordID, true))
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)

	// Drain any spurious recorded movements from the resolve path.
	f.repKeeper.burnCalls = nil
	f.repKeeper.unlockCalls = nil

	// Second prune: TTL still elapsed, status now ACTIVE → delete + unlock.
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err)

	require.Empty(t, f.repKeeper.burnCalls, "endorser DREAM must NOT be burned when appeal upheld")
	require.Len(t, f.repKeeper.unlockCalls, 1, "expected exactly one UnlockDREAM on endorser")
	require.True(t, f.repKeeper.unlockCalls[0].addr.Equals(f.memberAddr))
	require.Equal(t, stake.String(), f.repKeeper.unlockCalls[0].amount.String())
}

// Deferral resolves: appeal REJECTED (sentinel right) → collection deleted
// via ResolveHideAppeal callback path with status still HIDDEN. Slash fires
// on the endorser. Verifies the new ordering in callbacks.go (hr.Resolved
// pre-persisted before deleteCollectionFull) so the slash gate doesn't see
// this just-decided appeal as still in flight.
func TestPruneExpiredCollections_HiddenEndorsed_AppealRejected_Burns(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID, hideRecordID, stake := setupEndorsedHiddenAppealedCollection(t, f, 1000, 701)

	// Past TTL → first prune defers.
	f.setBlockHeight(1101)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	_, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)

	// Jury rejects — sentinel was right. ResolveHideAppeal deletes the
	// collection AND must trigger the slash (status still HIDDEN, appeal
	// just resolved).
	require.NoError(t, f.keeper.ResolveHideAppeal(f.ctx, hideRecordID, false))
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err)

	assertEndorsementSlashed(t, f, stake)
}

func TestPruneAppealTimeouts(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create an ACTIVE collection
	collID := f.createCollection(t, f.owner)

	// Hide it (sentinel)
	resp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	hideRecordID := resp.HideRecordId

	// Advance past appeal cooldown (default 600 blocks)
	f.advanceBlockHeight(601)

	// Appeal the hide (owner)
	_, err = f.msgServer.AppealHide(f.ctx, &types.MsgAppealHide{
		Creator:      f.owner,
		HideRecordId: hideRecordID,
	})
	require.NoError(t, err)

	// Verify appealed
	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Appealed)

	// Track refund and burn
	var refundCalled bool
	var burnCalled bool
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
		refundCalled = true
		return nil
	}
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}

	// Capture pre-prune commitment so we can verify ReleaseBond fires.
	preCommitted := f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, f.sentinel)].TotalCommittedBond

	// Advance past appeal_deadline_blocks (default 201600)
	// Appeal was filed, new deadline = current_block + 201600
	f.advanceBlockHeight(201601)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify hide record is resolved
	hr, err = f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)

	// Verify collection is restored to ACTIVE (appeal timeout favors appellant)
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)

	// Verify refund and burn happened (50% refund, 50% burn)
	require.True(t, refundCalled)
	require.True(t, burnCalled)

	// Verify sentinel bond released (TotalCommittedBond decreased).
	postCommitted := f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, f.sentinel)].TotalCommittedBond
	require.NotEqual(t, preCommitted, postCommitted, "expected ReleaseBond to reduce total_committed_bond")
}

func TestPruneExpiredFlags(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create an ACTIVE collection
	collID := f.createCollection(t, f.owner)

	// Flag it (member)
	_, err := f.msgServer.FlagContent(f.ctx, &types.MsgFlagContent{
		Creator:    f.member,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		Reason:     commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)

	// Verify flag exists
	actualFlagKey := fmtFlagKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, collID)
	_, err = f.keeper.Flag.Get(f.ctx, actualFlagKey)
	require.NoError(t, err)

	// Advance past flag_expiration_blocks (default 100800)
	// Flag was created at block 100, expiry = 100 + 100800 = 100900
	f.setBlockHeight(100901)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify flag is removed
	_, err = f.keeper.Flag.Get(f.ctx, actualFlagKey)
	require.Error(t, err)
}

func TestPruneUnendorsedCollections(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create a PENDING collection (nonMember)
	collID := f.createPendingCollection(t)

	// Verify it exists and is PENDING
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_PENDING, coll.Status)

	// Track refunds
	var refundCount int
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
		refundCount++
		return nil
	}

	// Advance past endorsement_expiry_blocks (default 432000)
	// Created at block 100, endorsement pending expiry = 100 + 432000 = 432100
	f.setBlockHeight(432101)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify collection is deleted
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err)

	// Verify refunds were called (endorsement creation fee + deposit)
	require.Greater(t, refundCount, 0)
}

func TestReleaseEndorsementStakes(t *testing.T) {
	f := initTestFixture(t)

	// Start at block 100
	f.setBlockHeight(100)

	// Create a PENDING collection via nonMember with a very long TTL
	// so the collection doesn't expire before the endorsement stake duration.
	// max_non_member_ttl_blocks = 432000, stake_duration = 432000, so set TTL = blockHeight + 432000
	msg := &types.MsgCreateCollection{
		Creator:    f.nonMember,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "endorse-stake-test",
		ExpiresAt:  100 + 432000, // maximum allowed for non-members
	}
	resp, err := f.msgServer.CreateCollection(f.ctx, msg)
	require.NoError(t, err)
	collID := resp.Id

	// Set seeking_endorsement = true
	_, err = f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
		Creator:      f.nonMember,
		CollectionId: collID,
		Seeking:      true,
	})
	require.NoError(t, err)

	// Endorse it (member) — endorsement changes status to ACTIVE
	_, err = f.msgServer.EndorseCollection(f.ctx, &types.MsgEndorseCollection{
		Creator:      f.member,
		CollectionId: collID,
	})
	require.NoError(t, err)

	// After endorsement, the collection is ACTIVE but still has a TTL.
	// The endorsement stake release is at block 100 + 432000 = 432100.
	// The TTL expiry is also at 432100. We need to remove the expiry index
	// so pruneExpiredCollections doesn't delete the collection first.
	// Or, we can simply test by advancing to exactly the stake release block
	// and verifying PruneExpired releases the stake.
	// Since both expire at 432100, the TTL prune will run first and delete the collection,
	// which also releases the endorsement stake via deleteCollectionFull.
	// So let's verify that the unlock is called through the TTL expiry path.

	// Verify endorsement exists
	endorsement, err := f.keeper.Endorsement.Get(f.ctx, collID)
	require.NoError(t, err)
	require.False(t, endorsement.StakeReleased)

	// Track DREAM unlock
	var unlockCalled bool
	var unlockAddr sdk.AccAddress
	var unlockAmount math.Int
	f.repKeeper.unlockDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
		unlockCalled = true
		unlockAddr = addr
		unlockAmount = amount
		return nil
	}

	// Advance past both TTL and stake duration (both at 432100)
	f.setBlockHeight(432101)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Verify DREAM was unlocked (either by deleteCollectionFull or releaseExpiredEndorsementStakes)
	require.True(t, unlockCalled)
	require.Equal(t, f.memberAddr.Bytes(), unlockAddr.Bytes())
	require.Equal(t, types.DefaultEndorsementDreamStake, unlockAmount)
}

func TestMaxPrunePerBlock(t *testing.T) {
	f := initTestFixture(t)

	// Set MaxPrunePerBlock to a small number
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPrunePerBlock = 3
	err = f.keeper.Params.Set(f.ctx, params)
	require.NoError(t, err)

	// Start at block 100
	f.setBlockHeight(100)

	// Create 5 TTL collections that all expire at block 200
	var collIDs []uint64
	for i := 0; i < 5; i++ {
		collID := f.createTTLCollection(t, f.owner, 200)
		collIDs = append(collIDs, collID)
	}

	// Advance past expiry
	f.setBlockHeight(201)

	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	// Count how many collections remain (cap=3, each 0-item collection costs 1)
	var remaining int
	for _, collID := range collIDs {
		_, err := f.keeper.Collection.Get(f.ctx, collID)
		if err == nil {
			remaining++
		}
	}
	require.Equal(t, 2, remaining, "MaxPrunePerBlock=3 should leave 2 of 5 collections un-pruned")

	// Run PruneExpired again to prune the rest
	err = f.keeper.PruneExpired(f.ctx)
	require.NoError(t, err)

	remaining = 0
	for _, collID := range collIDs {
		_, err := f.keeper.Collection.Get(f.ctx, collID)
		if err == nil {
			remaining++
		}
	}
	require.Equal(t, 0, remaining, "second pass should prune remaining collections")
}

// fmtFlagKey mirrors keeper.FlagCompositeKey without importing the internal package.
func fmtFlagKey(targetType types.FlagTargetType, targetID uint64) string {
	return fmt.Sprintf("%d/%d", int32(targetType), targetID)
}

// Verify the EndorsementPending index is cleaned up on endorsement
func TestEndorsementPendingCleanedUpOnEndorse(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createPendingCollection(t)

	// Verify EndorsementPending has an entry
	var pendingCount int
	f.keeper.EndorsementPending.Walk(f.ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K2() == collID {
			pendingCount++
		}
		return false, nil
	})
	require.Equal(t, 1, pendingCount)

	// Set seeking and endorse
	_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
		Creator: f.nonMember, CollectionId: collID, Seeking: true,
	})
	require.NoError(t, err)
	_, err = f.msgServer.EndorseCollection(f.ctx, &types.MsgEndorseCollection{
		Creator: f.member, CollectionId: collID,
	})
	require.NoError(t, err)

	// Verify EndorsementPending entry is removed
	pendingCount = 0
	f.keeper.EndorsementPending.Walk(f.ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K2() == collID {
			pendingCount++
		}
		return false, nil
	})
	require.Equal(t, 0, pendingCount)
}
