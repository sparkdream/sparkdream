package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/collect/types"
)

// withTags is a createCollection option that sets the collection's tags.
func withTags(tags ...string) createCollectionOpt {
	return func(msg *types.MsgCreateCollection) {
		msg.Tags = tags
	}
}

// setupEndorsedHiddenCollectionWithTags is the tagged variant of
// setupEndorsedHiddenCollection (endblock_test.go). The non-member-owned
// PENDING collection carries the given tags so the slash side-effects can
// assert per-tag rep deduction on the endorser.
func setupEndorsedHiddenCollectionWithTags(
	t *testing.T, f *testFixture, ttlBlocksFromNow int64, tags []string,
) (collID, hideRecordID uint64, stake math.Int) {
	t.Helper()

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	expiresAt := sdkCtx.BlockHeight() + ttlBlocksFromNow

	resp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.nonMember,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "endorsed-hidden-tagged",
		ExpiresAt:  expiresAt,
		Tags:       tags,
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

// filterRepDeductions returns the subset of recorded DeductReputation calls
// for which `pred` returns true. Keeps assertions specific when multiple slash
// paths fire in the same flow (e.g. author penalty AND endorser penalty share
// a fixture).
func filterRepDeductions(
	calls []repDeductionCall, pred func(repDeductionCall) bool,
) []repDeductionCall {
	out := make([]repDeductionCall, 0, len(calls))
	for _, c := range calls {
		if pred(c) {
			out = append(out, c)
		}
	}
	return out
}

// TestEndorserRepDeductedOnUnappealedHide is the per-tag rep-deduction counterpart
// to TestPruneUnappealedHides_EndorsedCollection_SlashesEndorser in endblock_test.go.
// The endorser staked DREAM on a sentinel-hidden collection that the owner
// declined to appeal; the §10.3 prune burns the stake AND must apply the
// per-tag EndorserRepPenalty against the endorser's score on each of the
// collection's tags.
func TestEndorserRepDeductedOnUnappealedHide(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	tags := []string{"alpha", "beta"}
	// TTL well past hide_expiry so §10.3 (unappealed-hide-expiry) wins, not §10.1.
	_, _, _ = setupEndorsedHiddenCollectionWithTags(t, f, 400_000, tags)

	// Drop the pre-test deduction recorder noise (the endorse + hide path itself
	// doesn't deduct, but be defensive against future churn).
	f.repKeeper.deductReputationCalls = nil

	// Owner does NOT appeal. Advance past hide_expiry deadline = 100 + 100800.
	f.setBlockHeight(100901)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	params, _ := f.keeper.Params.Get(f.ctx)
	endorserCalls := filterRepDeductions(f.repKeeper.deductReputationCalls,
		func(c repDeductionCall) bool { return c.addr.Equals(f.memberAddr) })
	require.Len(t, endorserCalls, len(tags),
		"expected one rep deduction per tag on the endorser; got %d calls", len(endorserCalls))

	seen := make(map[string]bool, len(tags))
	for _, c := range endorserCalls {
		require.True(t, c.amount.Equal(params.EndorserRepPenalty),
			"per-tag deduction amount must match EndorserRepPenalty; got %s want %s",
			c.amount, params.EndorserRepPenalty)
		seen[c.tag] = true
	}
	for _, tag := range tags {
		require.True(t, seen[tag], "expected deduction on tag %q", tag)
	}
}

// TestCollabInviterRepDeductedOnHiddenRemoval verifies the inviter's rep is
// deducted per-tag when the collaborator they vouched for is removed from a
// HIDDEN collection. Mirrors the "HIDDEN burns fraction, refunds rest" case
// in TestRemoveCollaborator but asserts the rep side-effect.
func TestCollabInviterRepDeductedOnHiddenRemoval(t *testing.T) {
	f := initTestFixture(t)
	tags := []string{"gamma"}

	// owner is the inviter; only owner / member / sentinel are members.
	f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
		return addr.Equals(f.ownerAddr) || addr.Equals(f.memberAddr) || addr.Equals(f.sentinelAddr)
	}

	collID := f.createCollection(t, f.owner, withTags(tags...))
	_, err := f.msgServer.AddCollaborator(f.ctx, &types.MsgAddCollaborator{
		Creator:      f.owner,
		CollectionId: collID,
		Address:      f.nonMember,
		Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
	})
	require.NoError(t, err)

	// Flip to HIDDEN — the fractional-burn branch in releaseOrSlashCollabStake
	// is the only path that triggers the rep deduction.
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
	require.NoError(t, f.keeper.Collection.Set(f.ctx, collID, coll))

	// Clear the recorder so only the remove-collaborator deductions count.
	f.repKeeper.deductReputationCalls = nil

	_, err = f.msgServer.RemoveCollaborator(f.ctx, &types.MsgRemoveCollaborator{
		Creator:      f.owner,
		CollectionId: collID,
		Address:      f.nonMember,
	})
	require.NoError(t, err)

	params, _ := f.keeper.Params.Get(f.ctx)
	inviterCalls := filterRepDeductions(f.repKeeper.deductReputationCalls,
		func(c repDeductionCall) bool { return c.addr.Equals(f.ownerAddr) })
	require.Len(t, inviterCalls, len(tags),
		"expected one rep deduction per tag on the inviter; got %d calls", len(inviterCalls))
	require.Equal(t, tags[0], inviterCalls[0].tag)
	require.True(t, inviterCalls[0].amount.Equal(params.CollabInviterRepPenalty),
		"deduction amount must match CollabInviterRepPenalty; got %s want %s",
		inviterCalls[0].amount, params.CollabInviterRepPenalty)
}

// TestAuthorRepDeductedOnSentinelHide verifies that when a sentinel hides a
// collection, the collection's creator gets a per-tag rep deduction equal to
// AuthorRepPenalty alongside the SlashAuthorBond side-effect. The penalty
// fires eagerly at hide time, matching the existing eager author-bond slash.
func TestAuthorRepDeductedOnSentinelHide(t *testing.T) {
	f := initTestFixture(t)
	tags := []string{"delta"}

	collID := f.createCollection(t, f.owner, withTags(tags...))

	// Clear any setup noise from CreateCollection.
	f.repKeeper.deductReputationCalls = nil

	_, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)

	params, _ := f.keeper.Params.Get(f.ctx)
	authorCalls := filterRepDeductions(f.repKeeper.deductReputationCalls,
		func(c repDeductionCall) bool { return c.addr.Equals(f.ownerAddr) })
	require.Len(t, authorCalls, len(tags),
		"expected one rep deduction per tag on the author; got %d calls", len(authorCalls))
	require.Equal(t, tags[0], authorCalls[0].tag)
	require.True(t, authorCalls[0].amount.Equal(params.AuthorRepPenalty),
		"deduction amount must match AuthorRepPenalty; got %s want %s",
		authorCalls[0].amount, params.AuthorRepPenalty)
}

// TestSlashRepPenalty_NilRepKeeperSafe verifies that the deductRepPerTag
// helper short-circuits cleanly when the rep keeper is nil. The path is
// indirect — there's no public entrypoint to the helper without the keeper
// wired — so we exercise the early-return guard by setting a zero penalty
// (semantically the same short-circuit branch) and confirming no calls fire.
func TestSlashRepPenalty_ZeroPenaltyShortCircuits(t *testing.T) {
	f := initTestFixture(t)

	// Override params so the endorser penalty is zero. Mirrors a governance
	// "disable this side-effect" knob.
	params, _ := f.keeper.Params.Get(f.ctx)
	params.EndorserRepPenalty = math.LegacyZeroDec()
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	f.setBlockHeight(100)
	_, _, _ = setupEndorsedHiddenCollectionWithTags(t, f, 400_000, []string{"alpha"})
	f.repKeeper.deductReputationCalls = nil

	f.setBlockHeight(100901)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	endorserCalls := filterRepDeductions(f.repKeeper.deductReputationCalls,
		func(c repDeductionCall) bool { return c.addr.Equals(f.memberAddr) })
	require.Empty(t, endorserCalls, "zero EndorserRepPenalty must short-circuit the deduction")
}

// --- Negative gates: the rep deduction must NOT fire on non-slash paths ---

// TestEndorserRepNotDeductedOnActiveCollectionDelete is the negative complement
// to TestEndorserRepDeductedOnUnappealedHide: when the owner deletes an
// ACTIVE endorsed collection (no sentinel hide), the endorser is REFUNDED
// (UnlockDREAM, not BurnDREAM) and rep MUST NOT be deducted. Regression guard
// for the `coll.Status == HIDDEN` gate in deleteCollectionFull's endorser branch.
func TestEndorserRepNotDeductedOnActiveCollectionDelete(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Build an endorsed PENDING collection, then promote it to ACTIVE by having
	// the endorsement fire — endorsement of a seeking PENDING flips the
	// collection's status to ACTIVE. Tags present so any spurious deduction
	// would be observable.
	collID, _, _ := setupEndorsedHiddenCollectionWithTags(t, f, 400_000, []string{"alpha", "beta"})

	// Restore status to ACTIVE — neutralizes the sentinel hide so the
	// MsgDeleteCollection takes the unlock branch, not the burn branch.
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.Status = types.CollectionStatus_COLLECTION_STATUS_ACTIVE
	require.NoError(t, f.keeper.Collection.Set(f.ctx, collID, coll))

	f.repKeeper.deductReputationCalls = nil

	// Owner deletes the now-ACTIVE collection.
	_, err := f.msgServer.DeleteCollection(f.ctx, &types.MsgDeleteCollection{
		Creator: f.nonMember,
		Id:      collID,
	})
	require.NoError(t, err)

	endorserCalls := filterRepDeductions(f.repKeeper.deductReputationCalls,
		func(c repDeductionCall) bool { return c.addr.Equals(f.memberAddr) })
	require.Empty(t, endorserCalls,
		"ACTIVE delete must take the unlock branch — no rep deduction on the endorser")
}

// TestAuthorRepNotDeductedOnItemHide verifies that when a sentinel hides an
// ITEM (not a collection), the author rep deduction does NOT fire. Items
// don't have author bonds; only collections do. Regression guard for the
// `msg.TargetType == FLAG_TARGET_TYPE_COLLECTION` gate in MsgHideContent.
func TestAuthorRepNotDeductedOnItemHide(t *testing.T) {
	f := initTestFixture(t)

	collID := f.createCollection(t, f.owner, withTags("gamma"))
	itemID := f.addItem(t, collID, f.owner)

	f.repKeeper.deductReputationCalls = nil

	_, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_ITEM,
		TargetId:   itemID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)

	require.Empty(t, f.repKeeper.deductReputationCalls,
		"item hide must not trigger author rep deduction — only collection hides do")
}

// TestCollabInviterRepNotDeductedOnActiveRemoval is the negative complement
// to TestCollabInviterRepDeductedOnHiddenRemoval: removing a non-member
// collaborator from an ACTIVE collection refunds the full stake and MUST NOT
// deduct the inviter's rep. Regression guard for the HIDDEN-status gate in
// releaseOrSlashCollabStake.
func TestCollabInviterRepNotDeductedOnActiveRemoval(t *testing.T) {
	f := initTestFixture(t)

	f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
		return addr.Equals(f.ownerAddr) || addr.Equals(f.memberAddr) || addr.Equals(f.sentinelAddr)
	}

	collID := f.createCollection(t, f.owner, withTags("gamma"))
	_, err := f.msgServer.AddCollaborator(f.ctx, &types.MsgAddCollaborator{
		Creator:      f.owner,
		CollectionId: collID,
		Address:      f.nonMember,
		Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
	})
	require.NoError(t, err)

	// Collection stays ACTIVE — no flip.
	f.repKeeper.deductReputationCalls = nil

	_, err = f.msgServer.RemoveCollaborator(f.ctx, &types.MsgRemoveCollaborator{
		Creator:      f.owner,
		CollectionId: collID,
		Address:      f.nonMember,
	})
	require.NoError(t, err)

	require.Empty(t, f.repKeeper.deductReputationCalls,
		"ACTIVE collaborator removal must not trigger inviter rep deduction")
}

// TestCollabInviterRepDeductedOnHiddenCollectionDeletion covers the SECOND
// path through releaseOrSlashCollabStake: deleteCollectionFull's collab walk
// firing on a HIDDEN collection. Same helper, different trigger from
// MsgRemoveCollaborator. Closes the "collab cleanup only fires the rep
// penalty when explicitly removed" gap. The collection here is a TTL-bound
// member collection (not the endorsed PENDING flow), so AddCollaborator is
// allowed before the manual HIDDEN flip.
func TestCollabInviterRepDeductedOnHiddenCollectionDeletion(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
		return addr.Equals(f.ownerAddr) || addr.Equals(f.memberAddr) || addr.Equals(f.sentinelAddr)
	}

	tags := []string{"gamma", "epsilon"}
	expiresAt := sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 1000
	collID := f.createCollection(t, f.owner, withTTL(expiresAt), withTags(tags...))

	_, err := f.msgServer.AddCollaborator(f.ctx, &types.MsgAddCollaborator{
		Creator:      f.owner,
		CollectionId: collID,
		Address:      f.nonMember,
		Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
	})
	require.NoError(t, err)

	// Manually flip to HIDDEN — bypasses the sentinel-hide flow so the test
	// stays focused on the collab walk inside deleteCollectionFull.
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
	require.NoError(t, f.keeper.Collection.Set(f.ctx, collID, coll))

	f.repKeeper.deductReputationCalls = nil

	// Trigger §10.1 TTL expiry on the HIDDEN collection — routes through
	// deleteCollectionFull and walks collaborators.
	f.setBlockHeight(expiresAt + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	params, _ := f.keeper.Params.Get(f.ctx)
	inviterCalls := filterRepDeductions(f.repKeeper.deductReputationCalls,
		func(c repDeductionCall) bool {
			return c.addr.Equals(f.ownerAddr) && c.amount.Equal(params.CollabInviterRepPenalty)
		})
	require.Len(t, inviterCalls, len(tags),
		"collection deletion with a HIDDEN-stake collaborator must deduct inviter rep on each tag")
}

// TestSlashRepPenalty_NoTagsShortCircuits verifies the early-return guard for
// collections that carry no tags — the slash still completes (DREAM burn etc.)
// but the rep-deduction loop must not fire spurious calls. Covers the
// `len(tags) == 0` branch in deductRepPerTag.
func TestSlashRepPenalty_NoTagsShortCircuits(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// No tags on the endorsed collection.
	_, _, _ = setupEndorsedHiddenCollectionWithTags(t, f, 400_000, nil)
	f.repKeeper.deductReputationCalls = nil

	f.setBlockHeight(100901)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	endorserCalls := filterRepDeductions(f.repKeeper.deductReputationCalls,
		func(c repDeductionCall) bool { return c.addr.Equals(f.memberAddr) })
	require.Empty(t, endorserCalls,
		"a collection with no tags must short-circuit the rep deduction loop")
}
