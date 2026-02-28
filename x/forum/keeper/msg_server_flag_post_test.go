package keeper_test

import (
	"testing"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestFlagPost(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	tests := []struct {
		name        string
		msg         *types.MsgFlagPost
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful flag",
			msg: &types.MsgFlagPost{
				Creator:  testCreator2,
				PostId:   post.PostId,
				Category: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				Reason:   "This is spam",
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgFlagPost{
				Creator:  "invalid-address",
				PostId:   post.PostId,
				Category: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				Reason:   "Test",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "post not found",
			msg: &types.MsgFlagPost{
				Creator:  testCreator2,
				PostId:   9999,
				Category: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				Reason:   "Test",
			},
			expectError: true,
			errContains: "post not found",
		},
		{
			name: "flag hidden post",
			msg: &types.MsgFlagPost{
				Creator:  testCreator2,
				PostId:   post.PostId,
				Category: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				Reason:   "Test",
			},
			setup: func() {
				p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
				p.Status = types.PostStatus_POST_STATUS_HIDDEN
				_ = f.keeper.Post.Set(f.ctx, post.PostId, p)
			},
			expectError: true,
			errContains: "already hidden",
		},
		{
			name: "already flagged",
			msg: &types.MsgFlagPost{
				Creator:  testCreator2,
				PostId:   post.PostId,
				Category: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				Reason:   "Test",
			},
			setup: func() {
				// Reset post status
				p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
				p.Status = types.PostStatus_POST_STATUS_ACTIVE
				_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

				// Create an existing flag
				flag := types.PostFlag{
					PostId:      post.PostId,
					Flaggers:    []string{testCreator2},
					TotalWeight: "1",
				}
				_ = f.keeper.PostFlag.Set(f.ctx, post.PostId, flag)
			},
			expectError: true,
			errContains: "already flagged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset post status and flags
			p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
			p.Status = types.PostStatus_POST_STATUS_ACTIVE
			_ = f.keeper.Post.Set(f.ctx, post.PostId, p)
			_ = f.keeper.PostFlag.Remove(f.ctx, post.PostId)

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.FlagPost(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify flag was created
				flag, err := f.keeper.PostFlag.Get(f.ctx, post.PostId)
				require.NoError(t, err)
				require.Contains(t, flag.Flaggers, tt.msg.Creator)
			}
		})
	}
}

func TestFlagPostWeight(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// First flag
	_, err := f.msgServer.FlagPost(f.ctx, &types.MsgFlagPost{
		Creator:  testCreator2,
		PostId:   post.PostId,
		Category: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
		Reason:   "Spam content",
	})
	require.NoError(t, err)

	// Verify flag record
	flag, err := f.keeper.PostFlag.Get(f.ctx, post.PostId)
	require.NoError(t, err)
	require.Equal(t, 1, len(flag.Flaggers))
	require.Equal(t, testCreator2, flag.Flaggers[0])

	// Second flag from different user
	_, err = f.msgServer.FlagPost(f.ctx, &types.MsgFlagPost{
		Creator:  testSentinel,
		PostId:   post.PostId,
		Category: uint64(commontypes.ModerationReason_MODERATION_REASON_HARASSMENT),
		Reason:   "Harassing content",
	})
	require.NoError(t, err)

	// Verify flag record updated
	flag, err = f.keeper.PostFlag.Get(f.ctx, post.PostId)
	require.NoError(t, err)
	require.Equal(t, 2, len(flag.Flaggers))
	require.Contains(t, flag.Flaggers, testSentinel)
}
