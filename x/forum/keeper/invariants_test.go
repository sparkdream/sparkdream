package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// --- PostCounterInvariant ---

func TestPostCounterInvariant_NoViolation(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// PostSeq was primed to 1 in initFixture. Create posts with IDs below the sequence.
	post := f.createTestPost(t, testCreator, 0, 0) // gets ID from PostSeq.Next()
	_ = post

	// PostSeq should now be > post.PostId
	invariant := keeper.PostCounterInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "invariant should not be broken: %s", msg)
}

func TestPostCounterInvariant_PostIDExceedsCounter(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Force PostSeq to a low value
	err := f.keeper.PostSeq.Set(f.ctx, 5)
	require.NoError(t, err)

	// Manually insert a post with ID >= 5
	bigPost := types.Post{
		PostId:  10,
		Author:  testCreator,
		Content: "test",
		Status:  types.PostStatus_POST_STATUS_ACTIVE,
	}
	err = f.keeper.Post.Set(f.ctx, 10, bigPost)
	require.NoError(t, err)

	invariant := keeper.PostCounterInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "invariant should detect post ID >= PostSeq")
	require.Contains(t, msg, "post ID 10 >= PostSeq 5")
}

func TestPostCounterInvariant_EmptyStore(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	invariant := keeper.PostCounterInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "empty store should have no violations: %s", msg)
}

// --- BountyPostReferenceInvariant ---

func TestBountyPostReferenceInvariant_NoViolation(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create a post and a bounty that references it
	post := f.createTestPost(t, testCreator, 0, 0)
	_ = f.createTestBounty(t, testCreator, post.PostId, "1000")

	invariant := keeper.BountyPostReferenceInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "invariant should not be broken: %s", msg)
}

func TestBountyPostReferenceInvariant_MissingPost(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create a bounty referencing a non-existent post (ID 99999)
	_ = f.createTestBounty(t, testCreator, 99999, "1000")

	invariant := keeper.BountyPostReferenceInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "invariant should detect missing post reference")
	require.Contains(t, msg, "non-existent root post 99999")
}

func TestBountyPostReferenceInvariant_InactiveBountySkipped(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create an expired bounty referencing a non-existent post
	bountyID, err := f.keeper.BountySeq.Next(f.ctx)
	require.NoError(t, err)

	bounty := types.Bounty{
		Id:       bountyID,
		Creator:  testCreator,
		ThreadId: 99999, // non-existent
		Amount:   "1000",
		Status:   types.BountyStatus_BOUNTY_STATUS_EXPIRED, // not active
	}
	err = f.keeper.Bounty.Set(f.ctx, bountyID, bounty)
	require.NoError(t, err)

	invariant := keeper.BountyPostReferenceInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "non-active bounties should be skipped: %s", msg)
}

// --- SentinelBondStatusInvariant ---

func TestSentinelBondStatusInvariant_NormalSufficient(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// NORMAL status with bond >= 1000
	_ = f.createTestSentinel(t, testSentinel, "1500")

	invariant := keeper.SentinelBondStatusInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "NORMAL sentinel with bond >= 1000 should pass: %s", msg)
}

func TestSentinelBondStatusInvariant_NormalInsufficientBond(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// NORMAL status but bond < 1000 (violation)
	sa := types.SentinelActivity{
		Address:            testSentinel,
		CurrentBond:        "500",
		TotalCommittedBond: "0",
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
	}
	err := f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sa)
	require.NoError(t, err)

	invariant := keeper.SentinelBondStatusInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "NORMAL sentinel with bond < 1000 should be a violation")
	require.Contains(t, msg, "NORMAL status but bond 500 < 1000")
}

func TestSentinelBondStatusInvariant_RecoveryInRange(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// RECOVERY status with 500 <= bond < 1000
	sa := types.SentinelActivity{
		Address:            testSentinel,
		CurrentBond:        "750",
		TotalCommittedBond: "0",
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY,
	}
	err := f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sa)
	require.NoError(t, err)

	invariant := keeper.SentinelBondStatusInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "RECOVERY sentinel with bond in [500, 1000) should pass: %s", msg)
}

func TestSentinelBondStatusInvariant_RecoveryBondTooHigh(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// RECOVERY status but bond >= 1000 (should be NORMAL)
	sa := types.SentinelActivity{
		Address:            testSentinel,
		CurrentBond:        "1000",
		TotalCommittedBond: "0",
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY,
	}
	err := f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sa)
	require.NoError(t, err)

	invariant := keeper.SentinelBondStatusInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "RECOVERY sentinel with bond >= 1000 should be a violation")
	require.Contains(t, msg, "RECOVERY status but bond 1000 >= 1000")
}

func TestSentinelBondStatusInvariant_RecoveryBondTooLow(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// RECOVERY status but bond < 500 (should be DEMOTED)
	sa := types.SentinelActivity{
		Address:            testSentinel,
		CurrentBond:        "300",
		TotalCommittedBond: "0",
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY,
	}
	err := f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sa)
	require.NoError(t, err)

	invariant := keeper.SentinelBondStatusInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "RECOVERY sentinel with bond < 500 should be a violation")
	require.Contains(t, msg, "RECOVERY status but bond 300 < 500")
}

func TestSentinelBondStatusInvariant_DemotedInRange(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// DEMOTED status with bond < 500
	sa := types.SentinelActivity{
		Address:            testSentinel,
		CurrentBond:        "200",
		TotalCommittedBond: "0",
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED,
	}
	err := f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sa)
	require.NoError(t, err)

	invariant := keeper.SentinelBondStatusInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "DEMOTED sentinel with bond < 500 should pass: %s", msg)
}

func TestSentinelBondStatusInvariant_DemotedBondTooHigh(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// DEMOTED status but bond >= 500 (should be RECOVERY or NORMAL)
	sa := types.SentinelActivity{
		Address:            testSentinel,
		CurrentBond:        "600",
		TotalCommittedBond: "0",
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED,
	}
	err := f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sa)
	require.NoError(t, err)

	invariant := keeper.SentinelBondStatusInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "DEMOTED sentinel with bond >= 500 should be a violation")
	require.Contains(t, msg, "DEMOTED status but bond 600 >= 500")
}

func TestSentinelBondStatusInvariant_CommittedExceedsCurrent(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// committed_bond > current_bond (violation regardless of status)
	sa := types.SentinelActivity{
		Address:            testSentinel,
		CurrentBond:        "1000",
		TotalCommittedBond: "1500",
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
	}
	err := f.keeper.SentinelActivity.Set(f.ctx, testSentinel, sa)
	require.NoError(t, err)

	invariant := keeper.SentinelBondStatusInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "committed_bond > current_bond should be a violation")
	require.Contains(t, msg, "committed_bond 1500 > current_bond 1000")
}

// --- ThreadLockConsistencyInvariant ---

func TestThreadLockConsistencyInvariant_LockedWithRecord(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create a locked root post with a matching lock record
	post := f.createTestPost(t, testCreator, 0, 0) // root post (parentId=0)
	post.Locked = true
	err := f.keeper.Post.Set(f.ctx, post.PostId, post)
	require.NoError(t, err)

	lockRecord := types.ThreadLockRecord{
		RootId:     post.PostId,
		Sentinel:   testSentinel,
		LockReason: "spam thread",
	}
	err = f.keeper.ThreadLockRecord.Set(f.ctx, post.PostId, lockRecord)
	require.NoError(t, err)

	invariant := keeper.ThreadLockConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "locked post with record should pass: %s", msg)
}

func TestThreadLockConsistencyInvariant_LockedMissingRecord(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create a locked root post WITHOUT a lock record
	post := f.createTestPost(t, testCreator, 0, 0)
	post.Locked = true
	err := f.keeper.Post.Set(f.ctx, post.PostId, post)
	require.NoError(t, err)

	invariant := keeper.ThreadLockConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "locked post without lock record should be a violation")
	require.Contains(t, msg, "locked but has no ThreadLockRecord")
}

func TestThreadLockConsistencyInvariant_RecordForNonExistentPost(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create a lock record for a post that doesn't exist
	lockRecord := types.ThreadLockRecord{
		RootId:     99999,
		Sentinel:   testSentinel,
		LockReason: "test",
	}
	err := f.keeper.ThreadLockRecord.Set(f.ctx, 99999, lockRecord)
	require.NoError(t, err)

	invariant := keeper.ThreadLockConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "lock record for non-existent post should be a violation")
	require.Contains(t, msg, "non-existent root post 99999")
}

func TestThreadLockConsistencyInvariant_RecordForUnlockedPost(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create an unlocked root post with a lock record (inconsistent)
	post := f.createTestPost(t, testCreator, 0, 0)
	// post.Locked is false by default

	lockRecord := types.ThreadLockRecord{
		RootId:     post.PostId,
		Sentinel:   testSentinel,
		LockReason: "test",
	}
	err := f.keeper.ThreadLockRecord.Set(f.ctx, post.PostId, lockRecord)
	require.NoError(t, err)

	invariant := keeper.ThreadLockConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "lock record for unlocked post should be a violation")
	require.Contains(t, msg, "post.Locked=false")
}

// --- HideRecordConsistencyInvariant ---

func TestHideRecordConsistencyInvariant_HiddenWithRecord(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create a hidden post with a matching hide record
	post := f.createTestPost(t, testCreator, 0, 0)
	post.Status = types.PostStatus_POST_STATUS_HIDDEN
	err := f.keeper.Post.Set(f.ctx, post.PostId, post)
	require.NoError(t, err)

	hideRecord := types.HideRecord{
		PostId:     post.PostId,
		Sentinel:   testSentinel,
		ReasonText: "inappropriate content",
	}
	err = f.keeper.HideRecord.Set(f.ctx, post.PostId, hideRecord)
	require.NoError(t, err)

	invariant := keeper.HideRecordConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "hidden post with record should pass: %s", msg)
}

func TestHideRecordConsistencyInvariant_NonHiddenWithRecord(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create an active post with a hide record (inconsistent)
	post := f.createTestPost(t, testCreator, 0, 0)
	// post.Status is ACTIVE by default

	hideRecord := types.HideRecord{
		PostId:     post.PostId,
		Sentinel:   testSentinel,
		ReasonText: "test",
	}
	err := f.keeper.HideRecord.Set(f.ctx, post.PostId, hideRecord)
	require.NoError(t, err)

	invariant := keeper.HideRecordConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "hide record for non-hidden post should be a violation")
	require.Contains(t, msg, "status is POST_STATUS_ACTIVE")
}

func TestHideRecordConsistencyInvariant_RecordForNonExistentPost(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create a hide record for a non-existent post
	hideRecord := types.HideRecord{
		PostId:     99999,
		Sentinel:   testSentinel,
		ReasonText: "test",
	}
	err := f.keeper.HideRecord.Set(f.ctx, 99999, hideRecord)
	require.NoError(t, err)

	invariant := keeper.HideRecordConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "hide record for non-existent post should be a violation")
	require.Contains(t, msg, "non-existent post 99999")
}

func TestHideRecordConsistencyInvariant_EmptyStore(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	invariant := keeper.HideRecordConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "empty store should have no violations: %s", msg)
}
