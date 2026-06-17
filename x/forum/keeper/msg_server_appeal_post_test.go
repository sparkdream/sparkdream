package keeper_test

import (
	"context"
	"fmt"
	"testing"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestAppealPost(t *testing.T) {
	f := initFixture(t)

	// Create a category and hidden post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Hide the post and create hide record
	p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
	p.Status = types.PostStatus_POST_STATUS_HIDDEN
	p.HiddenBy = testSentinel
	_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

	hideRecord := types.HideRecord{
		PostId:     post.PostId,
		Sentinel:   testSentinel,
		HiddenAt:   f.sdkCtx().BlockTime().Unix() - types.DefaultHideAppealCooldown - 1, // Past cooldown
		ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
		ReasonText: "Spam content",
	}
	_ = f.keeper.HideRecord.Set(f.ctx, post.PostId, hideRecord)

	// Create sentinel activity
	f.createTestSentinel(t, testSentinel, "2000000000")

	tests := []struct {
		name        string
		msg         *types.MsgAppealPost
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful appeal",
			msg: &types.MsgAppealPost{
				Creator: testCreator,
				PostId:  post.PostId,
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgAppealPost{
				Creator: "invalid-address",
				PostId:  post.PostId,
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "appeals paused",
			msg: &types.MsgAppealPost{
				Creator: testCreator,
				PostId:  post.PostId,
			},
			setup: func() {
				params := types.DefaultParams()
				params.AppealsPaused = true
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "appeals are paused",
		},
		{
			name: "post not found",
			msg: &types.MsgAppealPost{
				Creator: testCreator,
				PostId:  9999,
			},
			expectError: true,
			errContains: "post not found",
		},
		{
			name: "post not hidden",
			msg: &types.MsgAppealPost{
				Creator: testCreator,
				PostId:  post.PostId,
			},
			setup: func() {
				p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
				p.Status = types.PostStatus_POST_STATUS_ACTIVE
				_ = f.keeper.Post.Set(f.ctx, post.PostId, p)
			},
			expectError: true,
			errContains: "can only appeal hidden posts",
		},
		{
			name: "not the post author",
			msg: &types.MsgAppealPost{
				Creator: testCreator2,
				PostId:  post.PostId,
			},
			expectError: true,
			errContains: "only the post author can appeal",
		},
		{
			name: "cooldown not passed",
			msg: &types.MsgAppealPost{
				Creator: testCreator,
				PostId:  post.PostId,
			},
			setup: func() {
				hr, _ := f.keeper.HideRecord.Get(f.ctx, post.PostId)
				hr.HiddenAt = f.sdkCtx().BlockTime().Unix() // Just hidden
				_ = f.keeper.HideRecord.Set(f.ctx, post.PostId, hr)
			},
			expectError: true,
			errContains: "must wait until",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset state
			_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
			p.Status = types.PostStatus_POST_STATUS_HIDDEN
			p.Author = testCreator
			_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

			hideRecord := types.HideRecord{
				PostId:     post.PostId,
				Sentinel:   testSentinel,
				HiddenAt:   f.sdkCtx().BlockTime().Unix() - types.DefaultHideAppealCooldown - 1,
				ReasonCode: commontypes.ModerationReason_MODERATION_REASON_SPAM,
				ReasonText: "Spam content",
			}
			_ = f.keeper.HideRecord.Set(f.ctx, post.PostId, hideRecord)

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.AppealPost(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify sentinel activity was updated
				sentinel, err := f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
				require.NoError(t, err)
				require.Equal(t, uint64(1), sentinel.EpochAppealsFiled)
			}
		})
	}
}

func TestAppealPostNoHideRecord(t *testing.T) {
	// AppealPost rejects two flavors of non-sentinel hide. Both return
	// ErrGovLockNotAppealable but with different messages so operators can
	// see which path was taken.
	t.Run("hidden post with NO HideRecord at all (stale state)", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "NoHideRecord")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
		p.Status = types.PostStatus_POST_STATUS_HIDDEN
		_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

		_, err := f.msgServer.AppealPost(f.ctx, &types.MsgAppealPost{
			Creator: testCreator,
			PostId:  post.PostId,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGovLockNotAppealable)
		require.Contains(t, err.Error(), "no hide record found")
	})

	t.Run("gov-hide HideRecord (Sentinel empty marker)", func(t *testing.T) {
		// After the gov-hide-asymmetry fix, gov hides write a minimal
		// HideRecord with Sentinel == "". AppealPost must still reject these
		// as ErrGovLockNotAppealable — only sentinel hides are appealable
		// via this path.
		f := initFixture(t)
		cat := f.createTestCategory(t, "GovHideRecord")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
		p.Status = types.PostStatus_POST_STATUS_HIDDEN
		_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

		require.NoError(t, f.keeper.HideRecord.Set(f.ctx, post.PostId, types.HideRecord{
			PostId:   post.PostId,
			Sentinel: "", // gov-hide marker
			HiddenAt: f.sdkCtx().BlockTime().Unix(),
		}))

		_, err := f.msgServer.AppealPost(f.ctx, &types.MsgAppealPost{
			Creator: testCreator,
			PostId:  post.PostId,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGovLockNotAppealable)
		require.Contains(t, err.Error(), "governance authority hides")
	})
}

func TestAppealPostCreatesGovActionAppeal(t *testing.T) {
	f := initFixture(t)

	// The unified path routes hide appeals through x/rep's CreateGovActionAppeal
	// (POST_HIDE) rather than charging a forum-side fee. Track the rep call and
	// assert no forum fee is charged.
	var gotActionType reptypes.GovActionType
	var gotTarget string
	repCalled := false
	f.repKeeper.CreateGovActionAppealFn = func(ctx context.Context, actionType reptypes.GovActionType, actionTarget string, appellant sdk.AccAddress, reason string) (uint64, uint64, error) {
		repCalled = true
		gotActionType = actionType
		gotTarget = actionTarget
		return 7, 7, nil
	}
	forumFeeCharged := false
	f.bankKeeper.SendCoinsFromAccountToModuleFn = func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		forumFeeCharged = true
		return nil
	}

	// Create a category and hidden post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Hide the post and create hide record
	p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
	p.Status = types.PostStatus_POST_STATUS_HIDDEN
	_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

	hideRecord := types.HideRecord{
		PostId:   post.PostId,
		Sentinel: testSentinel,
		HiddenAt: f.sdkCtx().BlockTime().Unix() - types.DefaultHideAppealCooldown - 1,
	}
	_ = f.keeper.HideRecord.Set(f.ctx, post.PostId, hideRecord)

	// Create sentinel
	f.createTestSentinel(t, testSentinel, "2000000000")

	// File appeal
	_, err := f.msgServer.AppealPost(f.ctx, &types.MsgAppealPost{
		Creator: testCreator,
		PostId:  post.PostId,
	})
	require.NoError(t, err)

	require.True(t, repCalled, "should route through CreateGovActionAppeal")
	require.Equal(t, reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE, gotActionType)
	require.Equal(t, fmt.Sprintf("%d", post.PostId), gotTarget)
	require.False(t, forumFeeCharged, "legacy forum appeal fee must no longer be charged")
}
