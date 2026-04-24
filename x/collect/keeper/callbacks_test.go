package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

func TestOnMembershipGranted(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create PENDING collections for nonMember
	collID1 := f.createPendingCollection(t)
	collID2 := f.createPendingCollection(t)

	// Verify both are PENDING and immutable=false (non-member collections are not immutable from creation,
	// but endorsement sets immutable=true. These are plain PENDING.)
	coll1, err := f.keeper.Collection.Get(f.ctx, collID1)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_PENDING, coll1.Status)

	coll2, err := f.keeper.Collection.Get(f.ctx, collID2)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_PENDING, coll2.Status)

	// Call OnMembershipGranted for nonMember
	err = f.keeper.OnMembershipGranted(f.ctx, f.nonMember)
	require.NoError(t, err)

	// Verify both transitioned to ACTIVE
	coll1, err = f.keeper.Collection.Get(f.ctx, collID1)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll1.Status)
	require.False(t, coll1.Immutable)
	require.False(t, coll1.SeekingEndorsement)

	coll2, err = f.keeper.Collection.Get(f.ctx, collID2)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll2.Status)
	require.False(t, coll2.Immutable)
	require.False(t, coll2.SeekingEndorsement)
}

func TestOnMembershipGranted_NoCollections(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Call OnMembershipGranted on an address with no collections — should be a no-op
	randomAddr := sdk.AccAddress([]byte("random______________"))
	randomStr, _ := f.addressCodec.BytesToString(randomAddr)

	err := f.keeper.OnMembershipGranted(f.ctx, randomStr)
	require.NoError(t, err)
}

func TestOnMembershipGranted_LiftImmutability(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create PENDING collection, set seeking, endorse it (makes it ACTIVE + immutable)
	collID := f.createPendingCollection(t)
	_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
		Creator: f.nonMember, CollectionId: collID, Seeking: true,
	})
	require.NoError(t, err)
	_, err = f.msgServer.EndorseCollection(f.ctx, &types.MsgEndorseCollection{
		Creator: f.member, CollectionId: collID,
	})
	require.NoError(t, err)

	// After endorsement: status=ACTIVE, immutable=true
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
	require.True(t, coll.Immutable)

	// Call OnMembershipGranted — should lift immutability
	err = f.keeper.OnMembershipGranted(f.ctx, f.nonMember)
	require.NoError(t, err)

	coll, err = f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.False(t, coll.Immutable)
}

func TestResolveChallengeResult_Upheld(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create an ACTIVE collection
	collID := f.createCollection(t, f.owner)

	// Register member as curator with bond of 500 DREAM
	f.registerCurator(t, f.member, 500)

	// Advance past min_curator_age_blocks (default 14400)
	f.advanceBlockHeight(14401)

	// Rate the collection (creates a curation review)
	rateResp, err := f.msgServer.RateCollection(f.ctx, &types.MsgRateCollection{
		Creator:      f.member,
		CollectionId: collID,
		Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
	})
	require.NoError(t, err)
	reviewID := rateResp.ReviewId

	// Create a challenger address
	challengerAddr := sdk.AccAddress([]byte("challenger__________"))
	challengerStr, _ := f.addressCodec.BytesToString(challengerAddr)

	// Make the challenger a member
	origIsMemberFn := f.repKeeper.isMemberFn
	f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
		if addr.Equals(challengerAddr) {
			return true
		}
		if origIsMemberFn != nil {
			return origIsMemberFn(nil, addr)
		}
		return false
	}

	// Challenge the review
	_, err = f.msgServer.ChallengeReview(f.ctx, &types.MsgChallengeReview{
		Creator:  challengerStr,
		ReviewId: reviewID,
		Reason:   "inaccurate review",
	})
	require.NoError(t, err)

	// Verify review is challenged
	review, err := f.keeper.CurationReview.Get(f.ctx, reviewID)
	require.NoError(t, err)
	require.True(t, review.Challenged)

	// Track mock UnlockDREAM calls (challenger reward + challenge deposit
	// refund both route through UnlockDREAM in the new design).
	var unlockDREAMCalls []struct {
		addr   sdk.AccAddress
		amount math.Int
	}
	f.repKeeper.unlockDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
		unlockDREAMCalls = append(unlockDREAMCalls, struct {
			addr   sdk.AccAddress
			amount math.Int
		}{addr, amount})
		return nil
	}

	// Resolve challenge as upheld (challenger wins)
	err = f.keeper.ResolveChallengeResult(f.ctx, reviewID, true)
	require.NoError(t, err)

	// Verify review is overturned
	review, err = f.keeper.CurationReview.Get(f.ctx, reviewID)
	require.NoError(t, err)
	require.True(t, review.Overturned)

	// Bond slash = CuratorSlashFraction × MinCuratorBond (committed at
	// challenge time, consumed here).
	expectedSlash := types.DefaultCuratorSlashFraction.MulInt(types.DefaultMinCuratorBond).TruncateInt()

	// Challenger rewarded + deposit refunded (2 UnlockDREAM calls expected).
	require.GreaterOrEqual(t, len(unlockDREAMCalls), 2)

	// Verify curator bond updated via the mock rep keeper (bonded role).
	br, err := f.repKeeper.GetBondedRole(f.ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR, f.member)
	require.NoError(t, err)
	currentBond, _ := math.NewIntFromString(br.CurrentBond)
	require.Equal(t, math.NewInt(500).Sub(expectedSlash), currentBond)
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	require.True(t, committed.IsZero())

	// Per-module activity counters bumped.
	activity, err := f.keeper.CuratorActivity.Get(f.ctx, f.member)
	require.NoError(t, err)
	require.Equal(t, uint64(1), activity.OverturnedReviews)
	require.Equal(t, uint64(1), activity.ConsecutiveOverturns)
}

func TestResolveChallengeResult_Rejected(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create an ACTIVE collection
	collID := f.createCollection(t, f.owner)

	// Register member as curator with bond of 500 DREAM
	f.registerCurator(t, f.member, 500)

	// Advance past min_curator_age_blocks
	f.advanceBlockHeight(14401)

	// Rate the collection
	rateResp, err := f.msgServer.RateCollection(f.ctx, &types.MsgRateCollection{
		Creator:      f.member,
		CollectionId: collID,
		Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
	})
	require.NoError(t, err)
	reviewID := rateResp.ReviewId

	// Create a challenger
	challengerAddr := sdk.AccAddress([]byte("challenger__________"))
	challengerStr, _ := f.addressCodec.BytesToString(challengerAddr)

	origIsMemberFn := f.repKeeper.isMemberFn
	f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
		if addr.Equals(challengerAddr) {
			return true
		}
		if origIsMemberFn != nil {
			return origIsMemberFn(nil, addr)
		}
		return false
	}

	// Challenge the review
	_, err = f.msgServer.ChallengeReview(f.ctx, &types.MsgChallengeReview{
		Creator:  challengerStr,
		ReviewId: reviewID,
		Reason:   "inaccurate review",
	})
	require.NoError(t, err)

	// Track mock calls
	var burnDREAMAddr sdk.AccAddress
	var burnDREAMAmount math.Int
	f.repKeeper.burnDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
		burnDREAMAddr = addr
		burnDREAMAmount = amount
		return nil
	}

	// Resolve challenge as rejected (curator wins)
	err = f.keeper.ResolveChallengeResult(f.ctx, reviewID, false)
	require.NoError(t, err)

	// Verify review is NOT overturned
	review, err := f.keeper.CurationReview.Get(f.ctx, reviewID)
	require.NoError(t, err)
	require.False(t, review.Overturned)
	require.True(t, review.Challenged) // still marked as challenged

	// Verify challenge deposit was burned from challenger
	require.Equal(t, challengerAddr.Bytes(), burnDREAMAddr.Bytes())
	require.Equal(t, types.DefaultChallengeDeposit, burnDREAMAmount)

	// Verify bonded role released the committed slash budget (no committed
	// remaining on rejected challenge) and bond is intact.
	br, err := f.repKeeper.GetBondedRole(f.ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR, f.member)
	require.NoError(t, err)
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	require.True(t, committed.IsZero())
	currentBond, _ := math.NewIntFromString(br.CurrentBond)
	require.Equal(t, math.NewInt(500), currentBond)

	// Per-module activity counters bumped.
	activity, err := f.keeper.CuratorActivity.Get(f.ctx, f.member)
	require.NoError(t, err)
	require.Equal(t, uint64(1), activity.UpheldReviews)
	require.Equal(t, uint64(1), activity.ConsecutiveUpheld)
}

func TestResolveHideAppeal_Upheld(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create an ACTIVE collection
	collID := f.createCollection(t, f.owner)

	// Hide it (sentinel)
	hideResp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	hideRecordID := hideResp.HideRecordId

	// Advance past appeal cooldown
	f.advanceBlockHeight(601)

	// Appeal (owner)
	_, err = f.msgServer.AppealHide(f.ctx, &types.MsgAppealHide{
		Creator:      f.owner,
		HideRecordId: hideRecordID,
	})
	require.NoError(t, err)

	// Track refund (80% appeal fee to appellant)
	var refundCalled bool
	var refundAddr sdk.AccAddress
	var refundAmount sdk.Coins
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, recipient sdk.AccAddress, amt sdk.Coins) error {
		refundCalled = true
		refundAddr = recipient
		refundAmount = amt
		return nil
	}

	// Track sentinel bond slash
	var slashCalled bool
	f.forumKeeper.slashBondCommitmentFn = func(_ context.Context, sentinel string, amount math.Int, mod string, refID uint64) error {
		slashCalled = true
		require.Equal(t, f.sentinel, sentinel)
		return nil
	}

	// Track burn (20% of appeal fee)
	var burnCalled bool
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}

	// Resolve appeal as upheld (appellant wins, sentinel was wrong)
	err = f.keeper.ResolveHideAppeal(f.ctx, hideRecordID, true)
	require.NoError(t, err)

	// Verify hide record is resolved
	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)

	// Verify collection is restored to ACTIVE
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)

	// Verify appellant got 80% refund
	require.True(t, refundCalled)
	require.Equal(t, f.ownerAddr.Bytes(), refundAddr.Bytes())
	expectedRefund := types.DefaultAppealFee.MulRaw(80).Quo(math.NewInt(100))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uspark", expectedRefund)), refundAmount)

	// Verify sentinel bond was slashed
	require.True(t, slashCalled)

	// Verify 20% was burned
	require.True(t, burnCalled)
}

func TestResolveHideAppeal_Rejected(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create an ACTIVE collection
	collID := f.createCollection(t, f.owner)

	// Hide it (sentinel)
	hideResp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	hideRecordID := hideResp.HideRecordId

	// Advance past appeal cooldown
	f.advanceBlockHeight(601)

	// Appeal (owner)
	_, err = f.msgServer.AppealHide(f.ctx, &types.MsgAppealHide{
		Creator:      f.owner,
		HideRecordId: hideRecordID,
	})
	require.NoError(t, err)

	// Track sentinel bond release (no slash)
	var bondReleased bool
	f.forumKeeper.releaseBondCommitmentFn = func(_ context.Context, sentinel string, _ math.Int, _ string, _ uint64) error {
		bondReleased = true
		require.Equal(t, f.sentinel, sentinel)
		return nil
	}

	// Track sentinel reward
	var sentinelRewarded bool
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, recipient sdk.AccAddress, amt sdk.Coins) error {
		if recipient.Equals(f.sentinelAddr) {
			sentinelRewarded = true
		}
		return nil
	}

	// Track burn (jury + remaining)
	var burnCalled bool
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}

	// Resolve appeal as rejected (sentinel wins, content should be deleted)
	err = f.keeper.ResolveHideAppeal(f.ctx, hideRecordID, false)
	require.NoError(t, err)

	// Verify hide record is resolved
	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)

	// Verify collection is deleted (sentinel was right)
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err)

	// Verify sentinel bond was released
	require.True(t, bondReleased)

	// Verify sentinel was rewarded (50% of appeal fee)
	require.True(t, sentinelRewarded)

	// Verify burn happened (jury 20% + burned 30%)
	require.True(t, burnCalled)
}
