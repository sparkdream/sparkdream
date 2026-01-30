package keeper_test

import (
	"testing"

	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestCreatePost(t *testing.T) {
	f := initFixture(t)

	// Create a category first
	cat := f.createTestCategory(t, "General")

	tests := []struct {
		name        string
		msg         *types.MsgCreatePost
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful root post creation",
			msg: &types.MsgCreatePost{
				Creator:    testCreator,
				CategoryId: cat.CategoryId,
				ParentId:   0,
				Content:    "This is a test post",
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgCreatePost{
				Creator:    "invalid-address",
				CategoryId: cat.CategoryId,
				ParentId:   0,
				Content:    "Test content",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "forum paused",
			msg: &types.MsgCreatePost{
				Creator:    testCreator,
				CategoryId: cat.CategoryId,
				ParentId:   0,
				Content:    "Test content",
			},
			setup: func() {
				params := types.DefaultParams()
				params.ForumPaused = true
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "forum is paused",
		},
		{
			name: "category not found",
			msg: &types.MsgCreatePost{
				Creator:    testCreator,
				CategoryId: 9999,
				ParentId:   0,
				Content:    "Test content",
			},
			expectError: true,
			errContains: "category not found",
		},
		{
			name: "empty content",
			msg: &types.MsgCreatePost{
				Creator:    testCreator,
				CategoryId: cat.CategoryId,
				ParentId:   0,
				Content:    "",
			},
			expectError: true,
			errContains: "content cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset params before each test
			_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.CreatePost(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
			}
		})
	}
}

func TestCreatePostReply(t *testing.T) {
	f := initFixture(t)

	// Create a category and root post
	cat := f.createTestCategory(t, "General")
	rootPost := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	tests := []struct {
		name        string
		msg         *types.MsgCreatePost
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful reply creation",
			msg: &types.MsgCreatePost{
				Creator:    testCreator2,
				CategoryId: cat.CategoryId,
				ParentId:   rootPost.PostId,
				Content:    "This is a reply",
			},
			expectError: false,
		},
		{
			name: "parent post not found",
			msg: &types.MsgCreatePost{
				Creator:    testCreator,
				CategoryId: cat.CategoryId,
				ParentId:   9999,
				Content:    "Reply to non-existent post",
			},
			expectError: true,
			errContains: "parent post not found",
		},
		{
			name: "reply to locked thread",
			msg: &types.MsgCreatePost{
				Creator:    testCreator2,
				CategoryId: cat.CategoryId,
				ParentId:   rootPost.PostId,
				Content:    "Reply to locked thread",
			},
			setup: func() {
				// Lock the root post
				post, _ := f.keeper.Post.Get(f.ctx, rootPost.PostId)
				post.Locked = true
				_ = f.keeper.Post.Set(f.ctx, rootPost.PostId, post)
			},
			expectError: true,
			errContains: "thread is locked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the root post lock state
			post, _ := f.keeper.Post.Get(f.ctx, rootPost.PostId)
			post.Locked = false
			_ = f.keeper.Post.Set(f.ctx, rootPost.PostId, post)

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.CreatePost(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
			}
		})
	}
}
