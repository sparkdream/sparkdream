package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
)

// Regression tests for the 2026-10-26 x/collect security review (Part 6,
// COL-01..COL-14 and the REP-R-07 collect site).

func setOpParam(t *testing.T, f *testFixture, mut func(p *types.Params)) {
	t.Helper()
	p, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	mut(&p)
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))
}

func endorse(t *testing.T, f *testFixture, collID uint64) {
	t.Helper()
	_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
		Creator: f.nonMember, CollectionId: collID, Seeking: true,
	})
	require.NoError(t, err)
	_, err = f.msgServer.EndorseCollection(f.ctx, &types.MsgEndorseCollection{
		Creator: f.member, CollectionId: collID,
	})
	require.NoError(t, err)
}

func pendingEntries(t *testing.T, f *testFixture, collID uint64) int {
	t.Helper()
	n := 0
	require.NoError(t, f.keeper.EndorsementPending.Walk(f.ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K2() == collID {
			n++
		}
		return false, nil
	}))
	return n
}

// COL-01: changing endorsement_expiry_blocks between creation and endorsement
// must not leave the creation-time pending entry behind, and the pruner must
// never delete an endorsed (ACTIVE) collection.
func TestCOL01_EndorsedCollectionSurvivesExpiryParamChange(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	// Pending deadline inside the collection's TTL (createPendingCollection
	// uses a 100000-block TTL).
	setOpParam(t, f, func(p *types.Params) { p.EndorsementExpiryBlocks = 50000 })
	collID := f.createPendingCollection(t)

	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, int64(100+50000), coll.EndorsementPendingUntil)

	setOpParam(t, f, func(p *types.Params) { p.EndorsementExpiryBlocks = 30000 })
	endorse(t, f, collID)
	require.Zero(t, pendingEntries(t, f, collID), "endorse must remove the creation-time entry")

	f.setBlockHeight(100 + 50000 + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	coll, err = f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err, "endorsed collection must not be pruned")
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
}

// COL-01: a stale pending entry pointing at a non-PENDING collection only
// loses the entry.
func TestCOL01_PrunerSkipsNonPendingCollection(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createCollection(t, f.owner)
	require.NoError(t, f.keeper.EndorsementPending.Set(f.ctx, collections.Join(int64(150), collID)))

	f.setBlockHeight(200)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	_, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err, "ACTIVE collection must not be deleted by the unendorsed pruner")
	require.Zero(t, pendingEntries(t, f, collID))
}

// COL-02: the endorsement fee refunded on delete is the amount escrowed at
// creation, not the (raised) current param.
func TestCOL02_DeleteRefundsEscrowedEndorsementFee(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createPendingCollection(t)

	setOpParam(t, f, func(p *types.Params) { p.EndorsementCreationFee = math.NewInt(100_000_000) })

	refunded := math.ZeroInt()
	burned := math.ZeroInt()
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, amt sdk.Coins) error {
		refunded = refunded.Add(amt.AmountOf("uspark"))
		return nil
	}
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, amt sdk.Coins) error {
		burned = burned.Add(amt.AmountOf("uspark"))
		return nil
	}

	_, err := f.msgServer.DeleteCollection(f.ctx, &types.MsgDeleteCollection{Creator: f.nonMember, Id: collID})
	require.NoError(t, err)

	fee := types.DefaultEndorsementCreationFee
	feeBurn := types.DefaultEndorsementDeletionBurnFraction.MulInt(fee).TruncateInt()
	require.Equal(t, types.DefaultBaseCollectionDeposit.Add(fee.Sub(feeBurn)), refunded)
	require.Equal(t, feeBurn, burned)
}

// COL-02: the endorser share is computed from the escrowed fee.
func TestCOL02_EndorserShareUsesEscrowedFee(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createPendingCollection(t)
	setOpParam(t, f, func(p *types.Params) { p.EndorsementCreationFee = math.NewInt(100_000_000) })

	paid := math.ZeroInt()
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, amt sdk.Coins) error {
		paid = paid.Add(amt.AmountOf("uspark"))
		return nil
	}
	endorse(t, f, collID)
	require.Equal(t, types.DefaultEndorsementFeeEndorserShare.MulInt(types.DefaultEndorsementCreationFee).TruncateInt(), paid)

	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.True(t, coll.EndorsementFeeEscrowed.IsZero(), "fee is settled on endorsement")
}

// COL-03: a self-corrected hide keeps its reservation until appeal_deadline
// even when the owner deletes the collection first, and is then released.
func TestCOL03_SelfCorrectedReservationReleasedAfterTargetDelete(t *testing.T) {
	f := initTestFixture(t)
	denyCouncil(f)
	f.setBlockHeight(100)
	collID := f.createCollection(t, f.owner)

	pre := sentinelCommitted(f)
	hrID := hideCollectionForUnhide(t, f, collID)
	_, err := f.msgServer.UnhideContent(f.ctx, &types.MsgUnhideContent{Creator: f.sentinel, HideRecordId: hrID})
	require.NoError(t, err)
	held := sentinelCommitted(f)
	require.NotEqual(t, pre, held, "self-correct retains the reservation")

	_, err = f.msgServer.DeleteCollection(f.ctx, &types.MsgDeleteCollection{Creator: f.owner, Id: collID})
	require.NoError(t, err)
	require.Equal(t, held, sentinelCommitted(f), "delete neither releases early nor drops the hold")

	hr, err := f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	f.setBlockHeight(hr.AppealDeadline + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	require.Equal(t, pre, sentinelCommitted(f), "reservation released at appeal_deadline")

	hr, err = f.keeper.HideRecord.Get(f.ctx, hrID)
	require.NoError(t, err)
	require.True(t, hr.CommittedAmount.IsZero(), "released amount is zeroed on the record")
}

// COL-04: items cannot be changed while the item or its collection is hidden.
func TestCOL04_HiddenContentIsFrozen(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createCollection(t, f.owner)
	itemID := f.addItem(t, collID, f.owner)

	_, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator: f.sentinel, TargetId: itemID, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_ITEM,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)

	_, err = f.msgServer.UpdateItem(f.ctx, &types.MsgUpdateItem{Creator: f.owner, Id: itemID, Title: "benign"})
	require.ErrorIs(t, err, types.ErrContentHidden)
	_, err = f.msgServer.RemoveItem(f.ctx, &types.MsgRemoveItem{Creator: f.owner, Id: itemID})
	require.ErrorIs(t, err, types.ErrContentHidden)

	// Hidden collection: no item may be added.
	coll2 := f.createCollection(t, f.owner)
	_, err = f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator: f.sentinel, TargetId: coll2, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	_, err = f.msgServer.AddItem(f.ctx, &types.MsgAddItem{Creator: f.owner, CollectionId: coll2, Title: "x"})
	require.ErrorIs(t, err, types.ErrContentHidden)
}

// COL-05: curation challenges have a production verdict path, gated on the
// committee policy (never an individual member); an upheld verdict mints the
// challenger's reward and refunds the locked deposit.
func TestCOL05_ResolveCurationChallenge(t *testing.T) {
	f := initTestFixture(t)
	_, reviewID, challengerAddr, challengerStr := challengeTimeoutSetup(t, f)
	_, err := f.msgServer.ChallengeReview(f.ctx, &types.MsgChallengeReview{
		Creator: challengerStr, ReviewId: reviewID, Reason: "inaccurate review",
	})
	require.NoError(t, err)

	// A single committee member is not the committee.
	f.commonsKeeper.isCouncilOrCommitteeFn = func(_ context.Context, addr string, _, _ string) bool {
		return addr == f.authority
	}
	_, err = f.msgServer.ResolveCurationChallenge(f.ctx, &types.MsgResolveCurationChallenge{
		Creator: f.owner, ReviewId: reviewID, Upheld: true,
	})
	require.ErrorIs(t, err, types.ErrUnauthorized)

	_, err = f.msgServer.ResolveCurationChallenge(f.ctx, &types.MsgResolveCurationChallenge{
		Creator: f.authority, ReviewId: reviewID, Upheld: true, Reason: "review was wrong",
	})
	require.NoError(t, err)

	review, err := f.keeper.CurationReview.Get(f.ctx, reviewID)
	require.NoError(t, err)
	require.True(t, review.Overturned)
	require.True(t, review.ChallengeDeposit.IsZero())

	slash := types.DefaultCuratorSlashFraction.MulInt(types.DefaultMinCuratorBond).TruncateInt()
	require.Len(t, f.repKeeper.mintCalls, 1)
	require.True(t, f.repKeeper.mintCalls[0].addr.Equals(challengerAddr))
	require.Equal(t, types.DefaultChallengeRewardFraction.MulInt(slash).TruncateInt(), f.repKeeper.mintCalls[0].amount)
	require.Len(t, f.repKeeper.unlockCalls, 1)
	require.Equal(t, types.DefaultChallengeDeposit, f.repKeeper.unlockCalls[0].amount)

	// A second verdict is refused.
	_, err = f.msgServer.ResolveCurationChallenge(f.ctx, &types.MsgResolveCurationChallenge{
		Creator: f.authority, ReviewId: reviewID, Upheld: false,
	})
	require.ErrorIs(t, err, types.ErrReviewNotFound)
}

// COL-07: the timeout refunds the deposit actually locked even after the
// challenge_deposit param changed.
func TestCOL07_TimeoutRefundsLockedDeposit(t *testing.T) {
	f := initTestFixture(t)
	_, reviewID, _, challengerStr := challengeTimeoutSetup(t, f)
	_, err := f.msgServer.ChallengeReview(f.ctx, &types.MsgChallengeReview{
		Creator: challengerStr, ReviewId: reviewID, Reason: "inaccurate review",
	})
	require.NoError(t, err)
	setOpParam(t, f, func(p *types.Params) { p.ChallengeDeposit = math.NewInt(999_000_000) })

	f.advanceBlockHeight(types.ChallengeResolutionTimeoutBlocks + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	require.Len(t, f.repKeeper.unlockCalls, 1)
	require.Equal(t, types.DefaultChallengeDeposit, f.repKeeper.unlockCalls[0].amount)
}

// REP-R-07 (collect site): a migrated curator cannot challenge their own
// review from the new key.
func TestREPR07_SelfChallengeAcrossMigrationRefused(t *testing.T) {
	f := initTestFixture(t)
	_, reviewID, _, challengerStr := challengeTimeoutSetup(t, f)
	f.repKeeper.sameMemberFn = func(_ context.Context, a, b string) bool {
		pair := func(x, y string) bool { return (a == x && b == y) || (a == y && b == x) }
		return a == b || pair(challengerStr, f.member)
	}
	_, err := f.msgServer.ChallengeReview(f.ctx, &types.MsgChallengeReview{
		Creator: challengerStr, ReviewId: reviewID, Reason: "self",
	})
	require.ErrorIs(t, err, types.ErrCannotChallengeSelf)
}

// COL-08: a third party making someone else's TTL collection permanent pays
// the burn itself; the owner's escrowed deposit is refunded.
func TestCOL08_ThirdPartyMakePermanentRefundsOwner(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createTTLCollection(t, f.owner, 10000)

	var fromCaller, toOwner math.Int
	fromCaller, toOwner = math.ZeroInt(), math.ZeroInt()
	f.bankKeeper.sendCoinsFromAccountToModuleFn = func(_ context.Context, sender sdk.AccAddress, _ string, amt sdk.Coins) error {
		if sender.Equals(f.memberAddr) {
			fromCaller = fromCaller.Add(amt.AmountOf("uspark"))
		}
		return nil
	}
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, recipient sdk.AccAddress, amt sdk.Coins) error {
		if recipient.Equals(f.ownerAddr) {
			toOwner = toOwner.Add(amt.AmountOf("uspark"))
		}
		return nil
	}

	_, err := f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{Creator: f.member, CollectionId: collID})
	require.NoError(t, err)
	require.Equal(t, types.DefaultBaseCollectionDeposit, fromCaller)
	require.Equal(t, types.DefaultBaseCollectionDeposit, toOwner)

	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Zero(t, coll.ExpiresAt)
}

// COL-09: hide reasons follow the flag rules.
func TestCOL09_HideReasonValidation(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createCollection(t, f.owner)
	hide := func(code commontypes.ModerationReason, text string) error {
		_, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
			Creator: f.sentinel, TargetId: collID, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			ReasonCode: code, ReasonText: text,
		})
		return err
	}
	require.ErrorIs(t, hide(commontypes.ModerationReason_MODERATION_REASON_OTHER, ""), types.ErrFlagReasonTextRequired)
	require.ErrorIs(t, hide(commontypes.ModerationReason_MODERATION_REASON_SPAM, "spam"), types.ErrFlagReasonTextForbidden)
	require.ErrorIs(t, hide(commontypes.ModerationReason(9999), ""), types.ErrInvalidFlagReason)
	long := make([]byte, types.DefaultMaxFlagReasonLength+1)
	for i := range long {
		long[i] = 'a'
	}
	require.ErrorIs(t, hide(commontypes.ModerationReason_MODERATION_REASON_OTHER, string(long)), types.ErrFlagReasonTextTooLong)
}

// COL-10: an expired collection deferred for an in-flight appeal does not
// block the expiry of collections queued behind it.
func TestCOL10_DeferredExpiryDoesNotBlockQueue(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	setOpParam(t, f, func(p *types.Params) { p.MaxPrunePerBlock = 1 })
	deferred := f.createTTLCollection(t, f.owner, 200)
	behind := f.createTTLCollection(t, f.owner, 201)

	coll, err := f.keeper.Collection.Get(f.ctx, deferred)
	require.NoError(t, err)
	coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
	require.NoError(t, f.keeper.Collection.Set(f.ctx, deferred, coll))
	hr := types.HideRecord{Id: 77, TargetId: deferred, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		Appealed: true, CommittedAmount: math.ZeroInt()}
	require.NoError(t, f.keeper.HideRecord.Set(f.ctx, hr.Id, hr))
	require.NoError(t, f.keeper.HideRecordByTarget.Set(f.ctx, collections.Join(
		keeper.HideRecordTargetCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, deferred), hr.Id)))

	f.setBlockHeight(300)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	_, err = f.keeper.Collection.Get(f.ctx, deferred)
	require.NoError(t, err, "appeal in flight: deferred")
	_, err = f.keeper.Collection.Get(f.ctx, behind)
	require.Error(t, err, "the collection behind the deferred one is pruned")
}

// COL-06: deleting collection 1 must not touch collaborators of collection
// 10 (key prefix "1/" vs "10/").
func TestCOL06_CollaboratorDeleteIsScopedToCollection(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	var first, tenth uint64
	for tenth == 0 {
		id := f.createCollection(t, f.owner)
		switch id {
		case 1:
			first = id
		case 10:
			tenth = id
		}
	}
	require.Equal(t, uint64(1), first)
	f.addCollaborator(t, first, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
	f.addCollaborator(t, tenth, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)

	_, err := f.msgServer.DeleteCollection(f.ctx, &types.MsgDeleteCollection{Creator: f.owner, Id: first})
	require.NoError(t, err)
	has, err := f.keeper.Collaborator.Has(f.ctx, keeper.CollaboratorCompositeKey(tenth, f.member))
	require.NoError(t, err)
	require.True(t, has)
	has, err = f.keeper.Collaborator.Has(f.ctx, keeper.CollaboratorCompositeKey(first, f.member))
	require.NoError(t, err)
	require.False(t, has)
}

// COL-11 / COL-12: reaction dedup markers survive genesis (and are dropped
// with their target); ItemsByOnChainRef is rebuilt on import and cleaned on
// delete; flag expiry keys come from the stored value.
func TestCOL11_COL12_ReactionsAndRefsRoundTrip(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createCollection(t, f.owner)
	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator: f.owner, CollectionId: collID, Title: "ref",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain:       &types.OnChainReference{Module: "forum", EntityType: "post", EntityId: "7"},
	})
	require.NoError(t, err)
	_, err = f.msgServer.UpvoteContent(f.ctx, &types.MsgUpvoteContent{
		Creator: f.member, TargetId: collID, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
	})
	require.NoError(t, err)
	_, err = f.msgServer.FlagContent(f.ctx, &types.MsgFlagContent{
		Creator: f.member, TargetId: collID, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		Reason: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)

	gen, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gen.ReactionDedups, 1)
	require.NotEmpty(t, gen.ReactionLimits)
	// A changed param must not move the imported flag's expiry key.
	gen.Params.FlagExpirationBlocks = 5

	g := initTestFixture(t)
	g.setBlockHeight(100)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gen))

	_, err = g.msgServer.UpvoteContent(g.ctx, &types.MsgUpvoteContent{
		Creator: f.member, TargetId: collID, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
	})
	require.ErrorIs(t, err, types.ErrAlreadyVoted, "dedup marker survives export/import")

	refs := 0
	require.NoError(t, g.keeper.ItemsByOnChainRef.Walk(g.ctx, nil, func(collections.Pair[string, uint64]) (bool, error) {
		refs++
		return false, nil
	}))
	require.Equal(t, 1, refs, "ItemsByOnChainRef rebuilt on import")

	flagKey := keeper.FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, collID)
	has, err := g.keeper.FlagExpiry.Has(g.ctx, collections.Join(int64(100)+types.DefaultFlagExpirationBlocks, flagKey))
	require.NoError(t, err)
	require.True(t, has, "flag expiry rebuilt from the stored height, not the imported param")

	// Delete drops the dedup marker, its reverse index and the ref entry.
	_, err = g.msgServer.DeleteCollection(g.ctx, &types.MsgDeleteCollection{Creator: f.owner, Id: collID})
	require.NoError(t, err)
	_, err = g.keeper.ReactionDedup.Get(g.ctx, keeper.ReactionDedupCompositeKey(f.member, types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, collID))
	require.Error(t, err)
	refs = 0
	require.NoError(t, g.keeper.ItemsByOnChainRef.Walk(g.ctx, nil, func(collections.Pair[string, uint64]) (bool, error) {
		refs++
		return false, nil
	}))
	require.Zero(t, refs)
	n := 0
	require.NoError(t, g.keeper.ReactionsByTarget.Walk(g.ctx, nil, func(collections.Pair[string, string]) (bool, error) {
		n++
		return false, nil
	}))
	require.Zero(t, n)
}

// COL-13: a zeroed (no longer ACTIVE) member is treated as a non-member: its
// new collection goes through the endorsement stage.
func TestCOL13_InactiveMemberGetsNonMemberTreatment(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
		// owner keeps its Member record in x/rep but is ZEROED: not active.
		return addr.Equals(f.memberAddr) || addr.Equals(f.sentinelAddr)
	}
	collID := f.createTTLCollection(t, f.owner, 100+1000)
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_PENDING, coll.Status)
}

// COL-14: zero daily limits are rejected instead of silently disabling the
// action.
func TestCOL14_ZeroDailyLimitsRejected(t *testing.T) {
	for _, mut := range []func(p *types.Params){
		func(p *types.Params) { p.MaxUpvotesPerDay = 0 },
		func(p *types.Params) { p.MaxDownvotesPerDay = 0 },
		func(p *types.Params) { p.MaxPinsPerDay = 0 },
		func(p *types.Params) { p.MaxMakePermanentPerDay = 0 },
	} {
		p := types.DefaultParams()
		mut(&p)
		require.Error(t, p.Validate())
	}
}

// Operational updates cannot push moderation clocks below the hard floor.
func TestOperationalParamsHardBounds(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.msgServer.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
		Authority: f.authority,
		OperationalParams: types.CollectOperationalParams{
			AppealCooldownBlocks: 20000,
		},
	})
	require.ErrorContains(t, err, "operational hard bounds")

	_, err = f.msgServer.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{
		Authority: f.authority,
		OperationalParams: types.CollectOperationalParams{
			SentinelCommitAmount: math.NewInt(1),
		},
	})
	require.ErrorContains(t, err, "operational hard bounds")
}
