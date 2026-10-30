package keeper_test

import (
	"strings"
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

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
			// Above int32 the category used to be truncated onto a valid code
			// (1<<32 + 1 read as 1).
			name: "category above int32 refused, not truncated",
			msg: &types.MsgFlagPost{
				Creator:  testCreator2,
				PostId:   post.PostId,
				Category: 1<<32 + uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				Reason:   "Test",
			},
			expectError: true,
			errContains: "invalid flag category",
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
				require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, p))
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
				require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, p))

				// Create an existing flag
				flag := types.PostFlag{
					PostId:      post.PostId,
					Flaggers:    []string{testCreator2},
					TotalWeight: "1",
				}
				require.NoError(t, f.keeper.PostFlag.Set(f.ctx, post.PostId, flag))
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
			require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, p))
			require.NoError(t, f.keeper.PostFlag.Remove(f.ctx, post.PostId))

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

// MsgFlagPost input validation: category must be a concrete ModerationReason,
// OTHER needs reason text, the reason is length-capped, and authors cannot
// flag their own posts (anonymous flags via the shield account are exempt).
func TestFlagPost_Validation(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	anonPost := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	shieldAddr := authtypes.NewModuleAddress("shield").String()
	p, err := f.keeper.Post.Get(f.ctx, anonPost.PostId)
	require.NoError(t, err)
	p.Author = shieldAddr
	require.NoError(t, f.keeper.Post.Set(f.ctx, anonPost.PostId, p))

	spam := uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM)
	other := uint64(commontypes.ModerationReason_MODERATION_REASON_OTHER)

	tests := []struct {
		name    string
		creator string
		postID  uint64
		cat     uint64
		reason  string
		wantErr error
	}{
		{"unspecified category", testCreator2, post.PostId, 0, "x", types.ErrInvalidReasonCode},
		{"category past enum", testCreator2, post.PostId, other + 1, "x", types.ErrInvalidReasonCode},
		{"category overflows int32", testCreator2, post.PostId, 1 << 32, "x", types.ErrInvalidReasonCode},
		{"OTHER without reason", testCreator2, post.PostId, other, "", types.ErrReasonTextRequired},
		{"OTHER with whitespace reason", testCreator2, post.PostId, other, "   ", types.ErrReasonTextRequired},
		{"reason too long", testCreator2, post.PostId, spam, strings.Repeat("a", types.MaxFlagReasonLength+1), types.ErrFlagReasonTooLong},
		{"self flag", testCreator, post.PostId, spam, "mine", types.ErrCannotFlagOwnPost},
		{"reason at cap", testCreator2, post.PostId, spam, strings.Repeat("a", types.MaxFlagReasonLength), nil},
		{"OTHER with reason", testCreator2, post.PostId, other, "explained", nil},
		{"non-OTHER without reason", testCreator2, post.PostId, spam, "", nil},
		{"anonymous flag on anonymous post", shieldAddr, anonPost.PostId, spam, "anon", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, f.keeper.PostFlag.Remove(f.ctx, tt.postID))
			_, err := f.msgServer.FlagPost(f.ctx, &types.MsgFlagPost{
				Creator:  tt.creator,
				PostId:   tt.postID,
				Category: tt.cat,
				Reason:   tt.reason,
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				_, getErr := f.keeper.PostFlag.Get(f.ctx, tt.postID)
				require.Error(t, getErr, "rejected flag must not write a PostFlag")
				return
			}
			require.NoError(t, err)
			flag, err := f.keeper.PostFlag.Get(f.ctx, tt.postID)
			require.NoError(t, err)
			require.Contains(t, flag.Flaggers, tt.creator)
		})
	}
}
