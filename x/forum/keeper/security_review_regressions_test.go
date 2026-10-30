package keeper_test

// Regression tests for the 2026-10 security review of x/forum (FRM-01..24).
// Each test reproduces the reported failure and asserts the fixed behavior.

import (
	"context"
	"strconv"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

const reviewNow = int64(10_000_000)

func (f *fixture) at(ts int64) {
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(ts, 0))
}

func (f *fixture) committed(t *testing.T, sentinel string) math.Int {
	t.Helper()
	br := f.repKeeper.sentinels[sentinel]
	v, ok := math.NewIntFromString(br.TotalCommittedBond)
	require.True(t, ok)
	return v
}

func (f *fixture) govAuthority(t *testing.T) string {
	t.Helper()
	a, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)
	return a
}

func (f *fixture) sentinelHide(t *testing.T, sentinel string, postID uint64) {
	t.Helper()
	_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
		Creator:    sentinel,
		PostId:     postID,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
		Authority:  types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL,
	})
	require.NoError(t, err)
}

// FRM-01: archiving must not launder HIDDEN / DELETED replies back to ACTIVE,
// must refuse a thread with an unresolved hide, measure inactivity from the
// last reply and honor archive_cooldown.
func TestFRM01_FreezeUnarchiveKeepsModeratedStatus(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "archive")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	live := f.createTestPost(t, testCreator2, root.PostId, cat.CategoryId)
	deleted := f.createTestPost(t, testCreator2, root.PostId, cat.CategoryId)
	deleted.Status = types.PostStatus_POST_STATUS_DELETED
	require.NoError(t, f.keeper.Post.Set(f.ctx, deleted.PostId, deleted))

	// A pending hide in the thread blocks the archive.
	f.createTestSentinel(t, testSentinel, "2000000000")
	hidden := f.createTestPost(t, testCreator2, root.PostId, cat.CategoryId)
	f.sentinelHide(t, testSentinel, hidden.PostId)

	f.at(reviewNow + types.DefaultArchiveThreshold + 1)
	_, err := f.msgServer.FreezeThread(f.ctx, &types.MsgFreezeThread{Creator: testCreator2, RootId: root.PostId})
	require.ErrorIs(t, err, types.ErrCannotArchiveThreadWithPendingAppeal)

	// Settle the hide (expiry tombstones it: DELETED, record dropped).
	require.NoError(t, f.keeper.ExpireHiddenPosts(f.ctx, reviewNow+types.DefaultArchiveThreshold+int64(types.DefaultHiddenExpiration)))
	f.at(reviewNow + types.DefaultArchiveThreshold + int64(types.DefaultHiddenExpiration) + 1)

	_, err = f.msgServer.FreezeThread(f.ctx, &types.MsgFreezeThread{Creator: testCreator2, RootId: root.PostId})
	require.NoError(t, err)
	for id, want := range map[uint64]types.PostStatus{
		root.PostId:    types.PostStatus_POST_STATUS_ARCHIVED,
		live.PostId:    types.PostStatus_POST_STATUS_ARCHIVED,
		deleted.PostId: types.PostStatus_POST_STATUS_DELETED,
		hidden.PostId:  types.PostStatus_POST_STATUS_DELETED,
	} {
		p, err := f.keeper.Post.Get(f.ctx, id)
		require.NoError(t, err)
		require.Equal(t, want, p.Status, "post %d after freeze", id)
	}

	f.at(f.sdkCtx().BlockTime().Unix() + types.DefaultUnarchiveCooldown + 1)
	_, err = f.msgServer.UnarchiveThread(f.ctx, &types.MsgUnarchiveThread{Creator: testCreator2, RootId: root.PostId})
	require.NoError(t, err)
	p, _ := f.keeper.Post.Get(f.ctx, deleted.PostId)
	require.Equal(t, types.PostStatus_POST_STATUS_DELETED, p.Status, "a deleted reply must not come back")
	p, _ = f.keeper.Post.Get(f.ctx, live.PostId)
	require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, p.Status)

	// archive_cooldown: no immediate re-archive.
	_, err = f.msgServer.FreezeThread(f.ctx, &types.MsgFreezeThread{Creator: testCreator2, RootId: root.PostId})
	require.ErrorIs(t, err, types.ErrArchiveCooldown)
}

func TestFRM01_FreezeUsesLastActivityAndRefusesHiddenRoot(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "archive2")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// A fresh reply keeps an old thread alive.
	f.at(reviewNow + types.DefaultArchiveThreshold)
	f.createTestPost(t, testCreator2, root.PostId, cat.CategoryId)
	f.at(reviewNow + types.DefaultArchiveThreshold + 10)
	_, err := f.msgServer.FreezeThread(f.ctx, &types.MsgFreezeThread{Creator: testCreator2, RootId: root.PostId})
	require.ErrorIs(t, err, types.ErrThreadNotInactive)

	// A hidden root cannot be archived at all.
	root2 := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	root2.Status = types.PostStatus_POST_STATUS_HIDDEN
	require.NoError(t, f.keeper.Post.Set(f.ctx, root2.PostId, root2))
	f.at(reviewNow + 3*types.DefaultArchiveThreshold)
	_, err = f.msgServer.FreezeThread(f.ctx, &types.MsgFreezeThread{Creator: testCreator2, RootId: root2.PostId})
	require.ErrorIs(t, err, types.ErrPostStatus)
}

// FRM-02: the forum appeal target refuses action types it does not own and
// ignores appeals that were not filed through its own handlers.
func TestFRM02_AdapterRejectsForeignAndUnfiledAppeals(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "adapter")
	postID := f.hidePostViaSentinel(t, testSentinel, "2000000000", cat.CategoryId)
	target := strconv.FormatUint(postID, 10)
	before := f.committed(t, testSentinel)

	for _, at := range []reptypes.GovActionType{
		reptypes.GovActionType_GOV_ACTION_TYPE_WARNING,
		reptypes.GovActionType_GOV_ACTION_TYPE_DEMOTION,
		reptypes.GovActionType_GOV_ACTION_TYPE_ZEROING,
		reptypes.GovActionType_GOV_ACTION_TYPE_TAG_REMOVAL,
		reptypes.GovActionType_GOV_ACTION_TYPE_FORUM_PAUSE,
		reptypes.GovActionType_GOV_ACTION_TYPE_REVIEW_REJECTION,
	} {
		s, err := f.keeper.GetActionSentinel(f.ctx, at, target)
		require.Error(t, err, at.String())
		require.Empty(t, s)
		c, err := f.keeper.GetActionCommittedAmount(f.ctx, at, target)
		require.Error(t, err)
		require.True(t, c.IsZero())
		require.Error(t, f.keeper.ReverseSentinelAction(f.ctx, at, target))
		require.Error(t, f.keeper.OnAppealOutcome(f.ctx, at, target, reptypes.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD))
	}

	// A POST_HIDE appeal not filed through MsgAppealPost (record not marked
	// appealed) is a no-op: no sentinel, no committed amount, no reversal.
	s, err := f.keeper.GetActionSentinel(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE, target)
	require.NoError(t, err)
	require.Empty(t, s)
	require.NoError(t, f.keeper.ReverseSentinelAction(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE, target))
	post, _ := f.keeper.Post.Get(f.ctx, postID)
	require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, post.Status)
	require.True(t, f.committed(t, testSentinel).Equal(before))

	// After the author files through the forum, the same lookups resolve.
	f.at(reviewNow + types.DefaultHideAppealCooldown + 1)
	_, err = f.msgServer.AppealPost(f.ctx, &types.MsgAppealPost{Creator: testCreator, PostId: postID})
	require.NoError(t, err)
	s, err = f.keeper.GetActionSentinel(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE, target)
	require.NoError(t, err)
	require.Equal(t, testSentinel, s)

	// One appeal per hide.
	_, err = f.msgServer.AppealPost(f.ctx, &types.MsgAppealPost{Creator: testCreator, PostId: postID})
	require.ErrorIs(t, err, types.ErrAppealPending)
}

// FRM-02 / FRM-04: an UPHELD pin dispute settles the reservation, so a later
// unpin cannot release it a second time.
func TestFRM04_PinUpheldThenUnpinNoDoubleRelease(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "pins")
	f.createTestSentinel(t, testSentinel, "2000000000")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	reply := f.createTestPost(t, testCreator2, root.PostId, cat.CategoryId)
	_, err := f.msgServer.PinReply(f.ctx, &types.MsgPinReply{Creator: testSentinel, ThreadId: root.PostId, ReplyId: reply.PostId})
	require.NoError(t, err)
	slash := types.DefaultParams().SentinelSlashAmountInt()
	require.True(t, f.committed(t, testSentinel).Equal(slash))

	_, err = f.msgServer.DisputePin(f.ctx, &types.MsgDisputePin{Creator: testCreator, ThreadId: root.PostId, ReplyId: reply.PostId, Reason: "x"})
	require.NoError(t, err)

	// Council cannot unpin while the dispute is pending.
	_, err = f.msgServer.UnpinReply(f.ctx, &types.MsgUnpinReply{Creator: f.govAuthority(t), ThreadId: root.PostId, ReplyId: reply.PostId})
	require.ErrorIs(t, err, types.ErrPinDisputed)

	// Simulate rep's UPHELD: rep releases the committed amount, then notifies.
	target := strconv.FormatUint(reply.PostId, 10)
	c, err := f.keeper.GetActionCommittedAmount(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN, target)
	require.NoError(t, err)
	require.NoError(t, f.repKeeper.ReleaseBond(f.ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, testSentinel, c))
	require.NoError(t, f.keeper.OnAppealOutcome(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN, target, reptypes.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD))
	require.True(t, f.committed(t, testSentinel).IsZero())

	// A second reservation elsewhere must survive the council unpin.
	other := f.createTestPost(t, testCreator2, 0, cat.CategoryId)
	f.sentinelHide(t, testSentinel, other.PostId)
	require.True(t, f.committed(t, testSentinel).Equal(slash))
	_, err = f.msgServer.UnpinReply(f.ctx, &types.MsgUnpinReply{Creator: f.govAuthority(t), ThreadId: root.PostId, ReplyId: reply.PostId})
	require.NoError(t, err)
	require.True(t, f.committed(t, testSentinel).Equal(slash), "unpin after a settled dispute must not release again")
}

// FRM-03: a second move cannot overwrite an open move record.
func TestFRM03_SecondMoveRefusedWhileRecordOpen(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	c1 := f.createTestCategory(t, "c1")
	c2 := f.createTestCategory(t, "c2")
	c3 := f.createTestCategory(t, "c3")
	f.createTestSentinel(t, testSentinel, "2000000000")
	f.createTestSentinel(t, testCreator2, "2000000000")
	root := f.createTestPost(t, testCreator, 0, c1.CategoryId)

	_, err := f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{Creator: testSentinel, RootId: root.PostId, NewCategoryId: c2.CategoryId, Reason: "r", Authority: types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL})
	require.NoError(t, err)

	_, err = f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{Creator: testCreator2, RootId: root.PostId, NewCategoryId: c3.CategoryId, Reason: "r", Authority: types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL})
	require.ErrorIs(t, err, types.ErrMoveRecordOpen)

	// With an appeal pending even the council must resolve the appeal first.
	f.at(reviewNow + types.DefaultMoveAppealCooldown + 1)
	_, err = f.msgServer.AppealThreadMove(f.ctx, &types.MsgAppealThreadMove{Creator: testCreator, RootId: root.PostId})
	require.NoError(t, err)
	_, err = f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{Creator: f.govAuthority(t), RootId: root.PostId, NewCategoryId: c3.CategoryId, Reason: "r", Authority: types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL})
	require.ErrorIs(t, err, types.ErrAppealPending)
	rec, err := f.keeper.ThreadMoveRecord.Get(f.ctx, root.PostId)
	require.NoError(t, err)
	require.Equal(t, testSentinel, rec.Sentinel)
	require.Equal(t, c1.CategoryId, rec.OriginalCategoryId)
}

// FRM-04: unappealed lock / move reservations are released at the appeal
// deadline; a council unlock settles the record; TIMEOUT releases and
// reverses.
func TestFRM04_LockMoveReservationLifecycle(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	c1 := f.createTestCategory(t, "l1")
	c2 := f.createTestCategory(t, "l2")
	f.createTestSentinel(t, testSentinel, "20000000000")
	slash := types.DefaultParams().SentinelSlashAmountInt()

	locked := f.createTestPost(t, testCreator, 0, c1.CategoryId)
	_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{Creator: testSentinel, RootId: locked.PostId, Reason: "r", Authority: types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL})
	require.NoError(t, err)
	moved := f.createTestPost(t, testCreator, 0, c1.CategoryId)
	_, err = f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{Creator: testSentinel, RootId: moved.PostId, NewCategoryId: c2.CategoryId, Reason: "r", Authority: types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL})
	require.NoError(t, err)
	require.True(t, f.committed(t, testSentinel).Equal(slash.MulRaw(2)))

	// Unappealed past the deadline: both reservations come back, the actions
	// stand.
	require.NoError(t, f.keeper.ExpireSentinelActionRecords(f.ctx, reviewNow+types.DefaultLockAppealDeadline))
	require.True(t, f.committed(t, testSentinel).IsZero())
	_, err = f.keeper.ThreadLockRecord.Get(f.ctx, locked.PostId)
	require.Error(t, err)
	p, _ := f.keeper.Post.Get(f.ctx, locked.PostId)
	require.True(t, p.Locked)

	// Council unlock of a sentinel lock settles the record.
	locked2 := f.createTestPost(t, testCreator, 0, c1.CategoryId)
	_, err = f.msgServer.LockThread(f.ctx, &types.MsgLockThread{Creator: testSentinel, RootId: locked2.PostId, Reason: "r", Authority: types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL})
	require.NoError(t, err)
	_, err = f.msgServer.UnlockThread(f.ctx, &types.MsgUnlockThread{Creator: f.govAuthority(t), RootId: locked2.PostId})
	require.NoError(t, err)
	require.True(t, f.committed(t, testSentinel).IsZero())
	_, err = f.keeper.ThreadLockRecord.Get(f.ctx, locked2.PostId)
	require.Error(t, err)

	// TIMEOUT on an appealed lock releases the reservation and unlocks.
	locked3 := f.createTestPost(t, testCreator, 0, c1.CategoryId)
	_, err = f.msgServer.LockThread(f.ctx, &types.MsgLockThread{Creator: testSentinel, RootId: locked3.PostId, Reason: "r", Authority: types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL})
	require.NoError(t, err)
	f.at(reviewNow + types.DefaultLockAppealCooldown + 1)
	_, err = f.msgServer.AppealThreadLock(f.ctx, &types.MsgAppealThreadLock{Creator: testCreator, RootId: locked3.PostId})
	require.NoError(t, err)
	target := strconv.FormatUint(locked3.PostId, 10)
	require.NoError(t, f.keeper.OnAppealOutcome(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK, target, reptypes.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT))
	require.True(t, f.committed(t, testSentinel).IsZero())
	p, _ = f.keeper.Post.Get(f.ctx, locked3.PostId)
	require.False(t, p.Locked)
	_, err = f.keeper.ThreadLockRecord.Get(f.ctx, locked3.PostId)
	require.Error(t, err)

	// UPHELD drops the record (lock stands; AppealPending never lingers).
	locked4 := f.createTestPost(t, testCreator, 0, c1.CategoryId)
	_, err = f.msgServer.LockThread(f.ctx, &types.MsgLockThread{Creator: testSentinel, RootId: locked4.PostId, Reason: "r", Authority: types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL})
	require.NoError(t, err)
	f.at(f.sdkCtx().BlockTime().Unix() + types.DefaultLockAppealCooldown + 1)
	_, err = f.msgServer.AppealThreadLock(f.ctx, &types.MsgAppealThreadLock{Creator: testCreator, RootId: locked4.PostId})
	require.NoError(t, err)
	require.NoError(t, f.keeper.OnAppealOutcome(f.ctx, reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK, strconv.FormatUint(locked4.PostId, 10), reptypes.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD))
	_, err = f.keeper.ThreadLockRecord.Get(f.ctx, locked4.PostId)
	require.Error(t, err)
	p, _ = f.keeper.Post.Get(f.ctx, locked4.PostId)
	require.True(t, p.Locked)
}

// FRM-05: a hidden ephemeral post reaching its TTL releases the hide's
// reservation; an appealed one is deferred until the verdict.
func TestFRM05_EphemeralPruneSettlesHide(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "eph")
	f.createTestSentinel(t, testSentinel, "2000000000")
	f.repKeeper.IsMemberFn = func(context.Context, sdk.AccAddress) bool { return false }

	mk := func() types.Post {
		p := f.createTestPost(t, testCreator2, 0, cat.CategoryId)
		p.ExpirationTime = reviewNow + types.DefaultEphemeralTTL
		require.NoError(t, f.keeper.Post.Set(f.ctx, p.PostId, p))
		require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(p.ExpirationTime, p.PostId)))
		return p
	}
	plain := mk()
	appealed := mk()
	f.sentinelHide(t, testSentinel, plain.PostId)
	f.sentinelHide(t, testSentinel, appealed.PostId)
	slash := types.DefaultParams().SentinelSlashAmountInt()
	require.True(t, f.committed(t, testSentinel).Equal(slash.MulRaw(2)))

	f.at(reviewNow + types.DefaultHideAppealCooldown + 1)
	_, err := f.msgServer.AppealPost(f.ctx, &types.MsgAppealPost{Creator: testCreator2, PostId: appealed.PostId})
	require.NoError(t, err)

	end := reviewNow + types.DefaultEphemeralTTL
	f.at(end)
	require.NoError(t, f.keeper.PruneExpiredPosts(f.ctx, end))

	_, err = f.keeper.Post.Get(f.ctx, plain.PostId)
	require.Error(t, err, "unappealed hidden ephemeral post is pruned")
	require.True(t, f.committed(t, testSentinel).Equal(slash), "its reservation is released")
	act, _ := f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
	require.Equal(t, uint64(1), act.PendingHideCount)

	p, err := f.keeper.Post.Get(f.ctx, appealed.PostId)
	require.NoError(t, err, "appealed hidden post waits for the verdict")
	require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, p.Status)
	_, err = f.keeper.HideRecord.Get(f.ctx, appealed.PostId)
	require.NoError(t, err)
}

// FRM-06: the sentinel cannot self-unhide (nor the council unhide) a hide that
// is under appeal.
func TestFRM06_NoUnhideWhileAppealed(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "unhide")
	postID := f.hidePostViaSentinel(t, testSentinel, "2000000000", cat.CategoryId)
	f.at(reviewNow + types.DefaultHideAppealCooldown + 1)
	_, err := f.msgServer.AppealPost(f.ctx, &types.MsgAppealPost{Creator: testCreator, PostId: postID})
	require.NoError(t, err)

	_, err = f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{Creator: testSentinel, PostId: postID})
	require.ErrorIs(t, err, types.ErrAppealPending)
	_, err = f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{Creator: f.govAuthority(t), PostId: postID})
	require.ErrorIs(t, err, types.ErrAppealPending)
	_, err = f.keeper.HideRecord.Get(f.ctx, postID)
	require.NoError(t, err)
}

// FRM-07: a reply's category is its thread's; a mismatched id is refused and
// the write gates use the thread's category.
func TestFRM07_ReplyCategoryBoundToThread(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	open := f.createTestCategory(t, "open")
	members := f.createTestCategory(t, "members")
	mc := members
	mc.MembersOnlyWrite = true
	f.commonsKeeper.setCategory(mc)
	root := f.createTestPost(t, testCreator, 0, members.CategoryId)

	f.repKeeper.IsMemberFn = func(_ context.Context, a sdk.AccAddress) bool { return !a.Equals(testCreator2Addr) }
	_, err := f.msgServer.CreatePost(f.ctx, &types.MsgCreatePost{Creator: testCreator2, CategoryId: open.CategoryId, ParentId: root.PostId, Content: "hi"})
	require.ErrorIs(t, err, types.ErrInvalidCategoryId)
	_, err = f.msgServer.CreatePost(f.ctx, &types.MsgCreatePost{Creator: testCreator2, CategoryId: 0, ParentId: root.PostId, Content: "hi"})
	require.ErrorIs(t, err, types.ErrMembersOnlyWrite)
}

// FRM-08 + FRM-11 + FRM-14: expiry walks the index, pays assigned awards and
// clears the thread's active-bounty marker.
func TestFRM11_ExpiredBountyPaysAssignedAwards(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "bounty")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	reply := f.createTestPost(t, testCreator2, root.PostId, cat.CategoryId)
	b := f.createTestBounty(t, testCreator, root.PostId, "1000")
	_, err := f.msgServer.AssignBountyToReply(f.ctx, &types.MsgAssignBountyToReply{Creator: testCreator, ThreadId: root.PostId, ReplyId: reply.PostId, Reason: "good"})
	require.NoError(t, err)

	var paidTo []string
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, to sdk.AccAddress, amt sdk.Coins) error {
		paidTo = append(paidTo, to.String()+":"+amt.String())
		return nil
	}
	require.NoError(t, f.keeper.ExpireBounties(f.ctx, b.ExpiresAt))
	require.Equal(t, []string{testCreator2 + ":1000uspark"}, paidTo, "the assigned winner is paid, not the creator")
	got, _ := f.keeper.Bounty.Get(f.ctx, b.Id)
	require.Equal(t, types.BountyStatus_BOUNTY_STATUS_AWARDED, got.Status)
	has, _ := f.keeper.ActiveBountyByThread.Has(f.ctx, root.PostId)
	require.False(t, has)
}

func TestFRM11_AssignRefusesHiddenReply(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "bounty2")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	reply := f.createTestPost(t, testCreator2, root.PostId, cat.CategoryId)
	reply.Status = types.PostStatus_POST_STATUS_HIDDEN
	require.NoError(t, f.keeper.Post.Set(f.ctx, reply.PostId, reply))
	f.createTestBounty(t, testCreator, root.PostId, "1000")
	_, err := f.msgServer.AssignBountyToReply(f.ctx, &types.MsgAssignBountyToReply{Creator: testCreator, ThreadId: root.PostId, ReplyId: reply.PostId})
	require.ErrorIs(t, err, types.ErrPostStatus)
}

func TestFRM14_CancelClearsActiveBountyIndex(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "bounty3")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	b := f.createTestBounty(t, testCreator, root.PostId, "1000")
	_, err := f.msgServer.CancelBounty(f.ctx, &types.MsgCancelBounty{Creator: testCreator, BountyId: b.Id})
	require.NoError(t, err)
	has, _ := f.keeper.ActiveBountyByThread.Has(f.ctx, root.PostId)
	require.False(t, has)
}

// FRM-09 / FRM-15: a released stake is still slashed when a hide stands, and
// a migrated staker can release.
func TestFRM09_ReleasedStakeSlashedOnTombstone(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "stake")
	f.createTestTag(t, "go")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	post.Tags = []string{"go"}
	require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, post))

	id, err := f.keeper.OpenPostConvictionStake(f.ctx, testCreator2Addr, post.PostId, math.NewInt(100_000_000))
	require.NoError(t, err)
	stake, _ := f.keeper.PostConvictionStake.Get(f.ctx, id)
	stake.AccruedRepPerTag = map[string]string{"go": "3.000000000000000000"}
	require.NoError(t, f.keeper.PostConvictionStake.Set(f.ctx, id, stake))

	// FRM-15: B (the migrated key) releases A's stake.
	migrated := sdk.AccAddress([]byte("zenith______________")).String()
	f.repKeeper.CanActAsFn = func(_ context.Context, signer, stored string) bool {
		return signer == stored || (signer == migrated && stored == testCreator2)
	}
	f.at(stake.UnlocksAt)
	require.NoError(t, f.keeper.ReleasePostConvictionStake(f.ctx, id, sdk.MustAccAddressFromBech32(migrated)))

	// A later standing hide still claws back the rep the stake produced.
	f.createTestSentinel(t, testSentinel, "2000000000")
	f.sentinelHide(t, testSentinel, post.PostId)
	require.NoError(t, f.keeper.SlashStakesForPost(f.ctx, post.PostId))
	require.NotEmpty(t, f.repKeeper.forumRepDeductCalls)
	require.Equal(t, "go", f.repKeeper.forumRepDeductCalls[0].Tag)
	_, err = f.keeper.PostConvictionStake.Get(f.ctx, id)
	require.Error(t, err, "slashed released stake is removed")
}

// FRM-10: a sentinel cannot curate their own reply or dismiss flags on their
// own post.
func TestFRM10_NoSelfCuration(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "cur")
	f.createTestSentinel(t, testSentinel, "2000000000")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	own := f.createTestPost(t, testSentinel, root.PostId, cat.CategoryId)
	_, err := f.msgServer.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{Creator: testSentinel, ThreadId: root.PostId, ReplyId: own.PostId})
	require.ErrorIs(t, err, types.ErrCannotCurateOwnContent)

	ownRoot := f.createTestPost(t, testSentinel, 0, cat.CategoryId)
	require.NoError(t, f.keeper.PostFlag.Set(f.ctx, ownRoot.PostId, types.PostFlag{PostId: ownRoot.PostId, InReviewQueue: true}))
	_, err = f.msgServer.DismissFlags(f.ctx, &types.MsgDismissFlags{Creator: testSentinel, PostId: ownRoot.PostId})
	require.ErrorIs(t, err, types.ErrCannotCurateOwnContent)
}

// FRM-10: an auto-confirm on a thread whose author never came back mints no
// reward and records no upheld tick.
func TestFRM10_SilentAuthorAutoConfirmUnrewarded(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "cur2")
	f.createTestSentinel(t, testSentinel, "2000000000")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	reply := f.createTestPost(t, testCreator2, root.PostId, cat.CategoryId)
	_, err := f.msgServer.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{Creator: testSentinel, ThreadId: root.PostId, ReplyId: reply.PostId})
	require.NoError(t, err)

	timeout := types.DefaultParams().AcceptProposalTimeout
	for i := int64(1); i <= 3; i++ {
		f.at(reviewNow + i*timeout + 1)
		require.NoError(t, f.keeper.ProcessProposalAutoConfirm(f.ctx, reviewNow+i*timeout+1))
	}
	md, _ := f.keeper.ThreadMetadata.Get(f.ctx, root.PostId)
	require.Equal(t, reply.PostId, md.AcceptedReplyId, "the reply is still accepted")
	require.Empty(t, f.repKeeper.mintDreamCalls, "no curation reward for a silent author")
	require.Zero(t, f.repKeeper.roleActivity(testSentinel).UpheldActions[reptypes.ActionKindForumCuration])
}

// FRM-12: genesis round-trips conviction stakes, votes and derived indexes.
func TestFRM12_GenesisRoundTrip(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "gen")
	f.createTestTag(t, "go")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	post.Tags = []string{"go"}
	post.ExpirationTime = reviewNow + 100
	require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, post))
	reply := f.createTestPost(t, testCreator2, post.PostId, cat.CategoryId)
	id, err := f.keeper.OpenPostConvictionStake(f.ctx, testCreator2Addr, post.PostId, math.NewInt(100_000_000))
	require.NoError(t, err)
	require.NoError(t, f.keeper.PostVote.Set(f.ctx, collections.Join(post.PostId, testCreator2)))

	gen, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gen.PostConvictionStakeList, 1)
	require.Len(t, gen.PostVoteList, 1)

	g := initFixture(t)
	g.at(reviewNow)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gen))
	_, err = g.keeper.PostConvictionStake.Get(g.ctx, id)
	require.NoError(t, err)
	has, _ := g.keeper.PostConvictionStakesByPost.Has(g.ctx, collections.Join(post.PostId, id))
	require.True(t, has)
	has, _ = g.keeper.PostVote.Has(g.ctx, collections.Join(post.PostId, testCreator2))
	require.True(t, has)
	has, _ = g.keeper.ExpirationQueue.Has(g.ctx, collections.Join(post.ExpirationTime, post.PostId))
	require.True(t, has)
	has, _ = g.keeper.PostsByRoot.Has(g.ctx, collections.Join(post.PostId, reply.PostId))
	require.True(t, has)
	next, err := g.keeper.PostConvictionStakeSeq.Peek(g.ctx)
	require.NoError(t, err)
	require.Greater(t, next, id)
}

// FRM-13: the integer sliding window agrees with exact rational arithmetic at
// the boundary.
func TestFRM13_SlidingWindowIntegerMath(t *testing.T) {
	const day = int64(86400)
	// cur=5, prev=10, half the window elapsed: 5 + 10*0.5 = 10 (at limit 10).
	require.True(t, keeper.SlidingWindowAtLimitForTest(5, 10, day/2, day, 10))
	require.False(t, keeper.SlidingWindowAtLimitForTest(5, 10, day/2+1, day, 10))
	require.False(t, keeper.SlidingWindowAtLimitForTest(9, 0, 0, day, 10))
	require.True(t, keeper.SlidingWindowAtLimitForTest(10, 0, day, day, 10))
	// Huge limits do not overflow.
	require.False(t, keeper.SlidingWindowAtLimitForTest(1, 1, 0, day, ^uint64(0)))
}

// FRM-16: ops-tunable params are honored and forum_paused stops content
// writes beyond creation.
func TestFRM16_ParamsHonoredAndPauseCoverage(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "p")
	params := types.DefaultParams()
	params.MaxContentSize = 8
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	_, err := f.msgServer.CreatePost(f.ctx, &types.MsgCreatePost{Creator: testCreator, CategoryId: cat.CategoryId, Content: "123456789"})
	require.ErrorIs(t, err, types.ErrContentTooLarge)

	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	params.ForumPaused = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	_, err = f.msgServer.EditPost(f.ctx, &types.MsgEditPost{Creator: testCreator, PostId: post.PostId, NewContent: "x"})
	require.ErrorIs(t, err, types.ErrForumPaused)
	_, err = f.msgServer.UpvotePost(f.ctx, &types.MsgUpvotePost{Creator: testCreator2, PostId: post.PostId})
	require.ErrorIs(t, err, types.ErrForumPaused)
	_, err = f.msgServer.FlagPost(f.ctx, &types.MsgFlagPost{Creator: testCreator2, PostId: post.PostId, Category: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM)})
	require.ErrorIs(t, err, types.ErrForumPaused)
}

// FRM-17: sentinel pins cannot target the sentinel's own reply and a thread
// holds a bounded number of pins.
func TestFRM17_PinGuards(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "pin")
	f.createTestSentinel(t, testSentinel, "20000000000")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	own := f.createTestPost(t, testSentinel, root.PostId, cat.CategoryId)
	_, err := f.msgServer.PinReply(f.ctx, &types.MsgPinReply{Creator: testSentinel, ThreadId: root.PostId, ReplyId: own.PostId})
	require.ErrorIs(t, err, types.ErrCannotPinOwnReply)

	for i := uint64(0); i < types.DefaultMaxPinnedRepliesPerThread; i++ {
		r := f.createTestPost(t, testCreator2, root.PostId, cat.CategoryId)
		_, err := f.msgServer.PinReply(f.ctx, &types.MsgPinReply{Creator: testSentinel, ThreadId: root.PostId, ReplyId: r.PostId})
		require.NoError(t, err)
	}
	r := f.createTestPost(t, testCreator2, root.PostId, cat.CategoryId)
	_, err = f.msgServer.PinReply(f.ctx, &types.MsgPinReply{Creator: testSentinel, ThreadId: root.PostId, ReplyId: r.PostId})
	require.ErrorIs(t, err, types.ErrMaxPinnedReplies)

	// Hide reason text is bounded like flag reasons.
	_, err = f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
		Creator: testSentinel, PostId: r.PostId,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_OTHER),
		ReasonText: string(make([]byte, types.MaxFlagReasonLength+1)),
		Authority:  types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL,
	})
	require.ErrorIs(t, err, types.ErrFlagReasonTooLong)
}

// FRM-20: a zeroed (non-active) member is treated as a non-member.
func TestFRM20_ZeroedMemberPostsAreEphemeral(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "z")
	f.repKeeper.IsActiveMemberFn = func(_ context.Context, a sdk.AccAddress) bool { return !a.Equals(testCreator2Addr) }
	_, err := f.msgServer.CreatePost(f.ctx, &types.MsgCreatePost{Creator: testCreator2, CategoryId: cat.CategoryId, Content: "spam"})
	require.NoError(t, err)
	var found bool
	require.NoError(t, f.keeper.Post.Walk(f.ctx, nil, func(_ uint64, p types.Post) (bool, error) {
		if p.Author == testCreator2 {
			found = true
			require.Positive(t, p.ExpirationTime, "zeroed member's post is ephemeral")
		}
		return false, nil
	}))
	require.True(t, found)
}

// FRM-21: council locks (no record) do not trip the lock invariant.
func TestFRM21_CouncilLockPassesInvariant(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "inv")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{Creator: f.govAuthority(t), RootId: root.PostId, Reason: "r", Authority: types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL})
	require.NoError(t, err)
	msg, broken := keeper.ThreadLockConsistencyInvariant(f.keeper)(f.sdkCtx())
	require.False(t, broken, msg)
}

// FRM-22 / FRM-23.
func TestFRM22_NoOpEditRefused(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "e")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	_, err := f.msgServer.EditPost(f.ctx, &types.MsgEditPost{Creator: testCreator, PostId: post.PostId, NewContent: post.Content})
	require.ErrorIs(t, err, types.ErrNoContentChange)
}

func TestFRM23_IdempotentMakePermanentIsFree(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "mp")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	params := types.DefaultParams()
	params.MaxMakePermanentPerDay = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	for i := 0; i < 3; i++ {
		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{Creator: testCreator2, PostId: post.PostId})
		require.NoError(t, err)
	}
	rl, err := f.keeper.UserRateLimit.Get(f.ctx, testCreator2)
	if err == nil {
		require.Zero(t, rl.MakePermanentCurrentEpochCount)
	}
}

// Ops params need an executed committee decision, not one member, and stay
// inside the compiled bounds.
func TestOpsParamsNeedPolicyAndBounds(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	member := testCreator
	f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, addr, _, _ string) bool { return addr == member }
	f.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(_ context.Context, addr, _, _ string) bool { return addr == testAuthority }
	op := types.DefaultParams().ExtractOperationalParams()

	_, err := f.msgServer.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: member, OperationalParams: op})
	require.ErrorIs(t, err, types.ErrInvalidSigner)

	bad := op
	bad.SpamTaxAmount = math.NewInt(1_000_000_000) // 1000 SPARK, above the committee ceiling
	_, err = f.msgServer.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: testAuthority, OperationalParams: bad})
	require.Error(t, err)

	_, err = f.msgServer.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: testAuthority, OperationalParams: op})
	require.NoError(t, err)
}

// markHideAppealed flags a hide as appealed through the forum, the state
// MsgAppealPost leaves behind; the appeal-target adapter only honors such
// hides.
func (f *fixture) markHideAppealed(t *testing.T, postID uint64) {
	t.Helper()
	hr, err := f.keeper.HideRecord.Get(f.ctx, postID)
	require.NoError(t, err)
	hr.Appealed = true
	require.NoError(t, f.keeper.HideRecord.Set(f.ctx, postID, hr))
}

// FRM-18: the composite queries return whole pages, not one record.
func TestFRM18_CompositeQueriesReturnPages(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	q := keeper.NewQueryServerImpl(f.keeper)
	cat := f.createTestCategory(t, "q")
	for i := 0; i < 3; i++ {
		p := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		p.Pinned = true
		require.NoError(t, f.keeper.Post.Set(f.ctx, p.PostId, p))
		require.NoError(t, f.keeper.PostsByPinned.Set(f.ctx, collections.Join(cat.CategoryId, p.PostId)))
		require.NoError(t, f.keeper.PostFlag.Set(f.ctx, p.PostId, types.PostFlag{PostId: p.PostId, InReviewQueue: true}))
	}
	pinned, err := q.PinnedPosts(f.ctx, &types.QueryPinnedPostsRequest{CategoryId: cat.CategoryId})
	require.NoError(t, err)
	require.Len(t, pinned.Posts, 3)
	top, err := q.TopPosts(f.ctx, &types.QueryTopPostsRequest{})
	require.NoError(t, err)
	require.Len(t, top.Posts, 3)
	queue, err := q.FlagReviewQueue(f.ctx, &types.QueryFlagReviewQueueRequest{})
	require.NoError(t, err)
	require.Len(t, queue.Flags, 3)
}
