package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
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
	f.registerCurator(t, f.member, 500_000_000)

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
	require.Equal(t, math.NewInt(500_000_000).Sub(expectedSlash), currentBond)
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	require.True(t, committed.IsZero())

	// Per-module activity counters bumped.
	activity, err := f.keeper.CuratorActivity.Get(f.ctx, f.member)
	require.NoError(t, err)
	require.Equal(t, uint64(1), activity.OverturnedReviews)
	require.Equal(t, uint64(1), activity.ConsecutiveOverturns)

	// ...and mirrored into x/rep's shared RoleActivity, which is what the
	// curator SPARK pool reads for windowed accuracy. The local counters above
	// only drive collect's own demotion streak; without this report a curator
	// would look permanently unchallenged and could never earn from the pool.
	require.Equal(t, []roleOutcomeCall{{
		roleType: reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
		addr:     f.member,
		kind:     reptypes.ActionKindCollectCuration,
		upheld:   false,
	}}, f.repKeeper.roleOutcomeCalls)
}

func TestResolveChallengeResult_Rejected(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create an ACTIVE collection
	collID := f.createCollection(t, f.owner)

	// Register member as curator with bond of 500 DREAM
	f.registerCurator(t, f.member, 500_000_000)

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
	require.Equal(t, math.NewInt(500_000_000), currentBond)

	// Per-module activity counters bumped.
	activity, err := f.keeper.CuratorActivity.Get(f.ctx, f.member)
	require.NoError(t, err)
	require.Equal(t, uint64(1), activity.UpheldReviews)
	require.Equal(t, uint64(1), activity.ConsecutiveUpheld)

	// The upheld side must reach the shared record too, or every curator's
	// windowed accuracy reads as 0% and the pool pays nobody.
	require.Equal(t, []roleOutcomeCall{{
		roleType: reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
		addr:     f.member,
		kind:     reptypes.ActionKindCollectCuration,
		upheld:   true,
	}}, f.repKeeper.roleOutcomeCalls)
}

// hideAndAppeal hides a fresh collection as the sentinel and appeals it as
// the owner, returning (collectionID, hideRecordID).
func hideAndAppeal(t *testing.T, f *testFixture) (uint64, uint64) {
	t.Helper()
	collID := f.createCollection(t, f.owner)
	hideResp, err := f.msgServer.HideContent(f.ctx, &types.MsgHideContent{
		Creator:    f.sentinel,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
	})
	require.NoError(t, err)
	f.advanceBlockHeight(601) // past appeal cooldown

	// Opening the appeal moves no funds through collect: x/rep charges its
	// own appeal bond.
	f.bankKeeper.sendCoinsFromAccountToModuleFn = func(context.Context, sdk.AccAddress, string, sdk.Coins) error {
		t.Fatal("collect must not escrow an appeal fee")
		return nil
	}
	_, err = f.msgServer.AppealHide(f.ctx, &types.MsgAppealHide{Creator: f.owner, HideRecordId: hideResp.HideRecordId})
	require.NoError(t, err)
	f.bankKeeper.sendCoinsFromAccountToModuleFn = nil

	hr, err := f.keeper.HideRecord.Get(f.ctx, hideResp.HideRecordId)
	require.NoError(t, err)
	require.True(t, hr.Appealed)
	require.Equal(t, uint64(len(f.repKeeper.appealCalls)), hr.AppealId)
	require.Equal(t, reptypes.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE.String()+":"+strconv.FormatUint(hr.Id, 10),
		f.repKeeper.appealCalls[len(f.repKeeper.appealCalls)-1])
	return collID, hideResp.HideRecordId
}

func TestRepAppeal_Overturned(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID, hideRecordID := hideAndAppeal(t, f)
	preSentinelBond := f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)].CurrentBond

	// x/rep verdict OVERTURNED: rep slashes, collect restores.
	resolveAppealViaRep(t, f, hideRecordID, true)

	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
	postSentinelBond := f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)].CurrentBond
	require.NotEqual(t, preSentinelBond, postSentinelBond, "expected SlashBond to reduce current_bond")

	// A second verdict callback is a no-op and reports no sentinel.
	target := keeper.NewRepAppealTarget(f.keeper)
	sentinel, err := target.GetActionSentinel(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE, strconv.FormatUint(hideRecordID, 10))
	require.NoError(t, err)
	require.Empty(t, sentinel)
}

func TestRepAppeal_Upheld(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID, hideRecordID := hideAndAppeal(t, f)
	preSentinelCommitted := f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)].TotalCommittedBond

	// x/rep verdict UPHELD: rep releases the bond, collect deletes.
	resolveAppealViaRep(t, f, hideRecordID, false)

	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err, "collection deleted (sentinel was right)")
	postSentinelCommitted := f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)].TotalCommittedBond
	require.NotEqual(t, preSentinelCommitted, postSentinelCommitted, "expected ReleaseBond to reduce total_committed_bond")
}

func TestRepAppeal_TimeoutRestoresAndReleases(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID, hideRecordID := hideAndAppeal(t, f)
	role := f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)]
	preSentinelCommitted, preSentinelBond := role.TotalCommittedBond, role.CurrentBond

	// collect's own EndBlocker never expires an appealed hide.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	f.advanceBlockHeight(params.HideExpiryBlocks + 10)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_HIDDEN, coll.Status)

	timeoutAppealViaRep(t, f, hideRecordID)
	coll, err = f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status, "timeout favors the appellant")
	hr, err := f.keeper.HideRecord.Get(f.ctx, hideRecordID)
	require.NoError(t, err)
	require.True(t, hr.Resolved)
	postSentinelCommitted := f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)].TotalCommittedBond
	require.NotEqual(t, preSentinelCommitted, postSentinelCommitted, "collect releases the bond on timeout")
	require.Equal(t, preSentinelBond, f.repKeeper.bondedRoles[mockBondedRoleKey(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, f.sentinel)].CurrentBond, "a timeout slashes no one")
}

func TestRepAppealTarget_RejectsForeignTypes(t *testing.T) {
	f := initTestFixture(t)
	target := keeper.NewRepAppealTarget(f.keeper)
	_, err := target.GetActionSentinel(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE, "1")
	require.Error(t, err)
	_, err = target.GetActionSentinel(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE, "nope")
	require.Error(t, err)
	// Unknown record ids are a soft no-op.
	require.NoError(t, target.ReverseSentinelAction(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE, "424242"))
}
