package keeper_test

import (
	"testing"
	"time"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

// govAuthAddr returns the bech32 address that the fixture's mockCommonsKeeper
// recognises as council-authorized for any (council, committee) pair. The
// fixture wires this in initFixture from authtypes.NewModuleAddress(GovModule).
func govAuthAddr() string {
	return authtypes.NewModuleAddress(types.GovModuleName).String()
}

// hidePost is a small helper that puts a fresh post into POST_STATUS_HIDDEN
// state by going through the real MsgHidePost handler. Returns the post id.
// Centralised so the auth/window/dangling subtests below all start from an
// identical baseline.
func (f *fixture) hidePostViaSentinel(t *testing.T, sentinel, sentinelBond string, categoryID uint64) uint64 {
	t.Helper()
	f.createTestSentinel(t, sentinel, sentinelBond)
	post := f.createTestPost(t, testCreator, 0, categoryID)
	_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
		Creator:    sentinel,
		PostId:     post.PostId,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
		ReasonText: "test",
	})
	require.NoError(t, err, "test setup: HidePost must succeed")
	return post.PostId
}

func TestMsgServerUnhidePost(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		_, err := f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: "not-a-bech32",
			PostId:  1,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("moderation paused", func(t *testing.T) {
		f := initFixture(t)
		params := types.DefaultParams()
		params.ModerationPaused = true
		require.NoError(t, f.keeper.Params.Set(f.ctx, params))

		_, err := f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: testSentinel,
			PostId:  1,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrModerationPaused)
	})

	t.Run("post not found", func(t *testing.T) {
		f := initFixture(t)
		_, err := f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: govAuthAddr(),
			PostId:  9_999_999,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("post not hidden", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "NotHiddenCat")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		_, err := f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: govAuthAddr(),
			PostId:  post.PostId,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotHidden)
	})

	t.Run("happy path: sentinel self-correct within window", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "SelfCorrectCat")
		postID := f.hidePostViaSentinel(t, testSentinel, "2000000000", cat.CategoryId)

		// Stay inside the default window — no time travel needed.
		_, err := f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: testSentinel,
			PostId:  postID,
		})
		require.NoError(t, err)

		post, err := f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, post.Status)
		require.Empty(t, post.HiddenBy)
		require.EqualValues(t, 0, post.HiddenAt)

		// HideRecord cleaned up.
		_, err = f.keeper.HideRecord.Get(f.ctx, postID)
		require.Error(t, err, "HideRecord must be removed after self-correct")

		// PendingHideCount decremented back to zero.
		sa, err := f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
		require.NoError(t, err)
		require.EqualValues(t, 0, sa.PendingHideCount)
	})

	t.Run("rejected: sentinel self-correct outside window", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "WindowExpiredCat")
		postID := f.hidePostViaSentinel(t, testSentinel, "2000000000", cat.CategoryId)

		// Advance block time past SentinelUnhideWindow (24h default).
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		future := sdkCtx.BlockTime().Add(25 * time.Hour)
		f.ctx = sdkCtx.WithBlockTime(future)

		_, err := f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: testSentinel,
			PostId:  postID,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUnhideWindowExpired)

		// Post must remain HIDDEN — no partial mutation.
		post, err := f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, post.Status)
	})

	t.Run("happy path: council unhide anytime (after window expired)", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "CouncilOverrideCat")
		postID := f.hidePostViaSentinel(t, testSentinel, "2000000000", cat.CategoryId)

		// Push way past the sentinel window — council should still succeed.
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		future := sdkCtx.BlockTime().Add(30 * 24 * time.Hour)
		f.ctx = sdkCtx.WithBlockTime(future)

		_, err := f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: govAuthAddr(),
			PostId:  postID,
		})
		require.NoError(t, err)

		post, err := f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, post.Status)
	})

	t.Run("rejected: random non-council, non-sentinel caller", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "RandomCallerCat")
		postID := f.hidePostViaSentinel(t, testSentinel, "2000000000", cat.CategoryId)

		_, err := f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: testCreator2, // not authority, not the sentinel
			PostId:  postID,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")

		post, err := f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, post.Status)
	})

	t.Run("rejected: post author cannot self-unhide", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "AuthorSelfUnhideCat")
		postID := f.hidePostViaSentinel(t, testSentinel, "2000000000", cat.CategoryId)

		_, err := f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: testCreator, // the author of the post
			PostId:  postID,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("rejected: parent category deleted (dangling reference guard)", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "WillBeDeletedCat")
		postID := f.hidePostViaSentinel(t, testSentinel, "2000000000", cat.CategoryId)

		// Simulate the category being deleted by x/commons after the hide.
		delete(f.commonsKeeper.categories, cat.CategoryId)

		_, err := f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: govAuthAddr(),
			PostId:  postID,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCategoryNotFound)

		// Post must remain HIDDEN — no partial mutation.
		post, err := f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, post.Status)
	})

	t.Run("council unhide of gov-hidden post works (minimal HideRecord cleaned up)", func(t *testing.T) {
		// Gov-authority hides now write a minimal HideRecord with Sentinel
		// == "" (gov-hide marker). The council unhide path treats that as
		// "no sentinel to release a bond to" — but still cleans up the
		// HideRecord and restores any snapshotted author bond.
		f := initFixture(t)
		cat := f.createTestCategory(t, "GovHiddenCat")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    govAuthAddr(),
			PostId:     post.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "gov hide",
		})
		require.NoError(t, err)

		// HideRecord exists with Sentinel == "" (gov-hide marker).
		hr, err := f.keeper.HideRecord.Get(f.ctx, post.PostId)
		require.NoError(t, err, "gov hide must write a minimal HideRecord")
		require.Empty(t, hr.Sentinel, "gov-hide marker: Sentinel must be empty")

		_, err = f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: govAuthAddr(),
			PostId:  post.PostId,
		})
		require.NoError(t, err)

		stored, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, stored.Status)

		// HideRecord must be cleaned up by the council unhide.
		_, err = f.keeper.HideRecord.Get(f.ctx, post.PostId)
		require.Error(t, err, "HideRecord must be removed after council unhide")
	})
}
