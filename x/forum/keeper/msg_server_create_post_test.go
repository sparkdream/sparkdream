package keeper_test

import (
	"context"
	"fmt"
	"testing"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

func TestCreatePostWithTags(t *testing.T) {
	f := initFixture(t)

	cat := f.createTestCategory(t, "General")

	// Create some tags in the store
	f.createTestTag(t, "golang")
	f.createTestTag(t, "cosmos-sdk")
	f.createTestTag(t, "testing")
	f.createTestTag(t, "alpha")
	f.createTestTag(t, "beta")
	f.createTestTag(t, "gamma")

	t.Run("successful post with tags", func(t *testing.T) {
		// Peek at the next post ID before creating
		nextID, _ := f.keeper.PostSeq.Peek(f.ctx)

		msg := &types.MsgCreatePost{
			Creator:    testCreator,
			CategoryId: cat.CategoryId,
			ParentId:   0,
			Content:    "Post with tags",
			Tags:       []string{"golang", "cosmos-sdk"},
		}
		_, err := f.msgServer.CreatePost(f.ctx, msg)
		require.NoError(t, err)

		// Verify tags are stored on the post
		post, err := f.keeper.Post.Get(f.ctx, nextID)
		require.NoError(t, err)
		require.Equal(t, []string{"golang", "cosmos-sdk"}, post.Tags)

		// Verify tag usage metadata was updated
		tag, err := f.keeper.Tag.Get(f.ctx, "golang")
		require.NoError(t, err)
		require.Equal(t, uint64(1), tag.UsageCount)
	})

	t.Run("successful post without tags", func(t *testing.T) {
		msg := &types.MsgCreatePost{
			Creator:    testCreator,
			CategoryId: cat.CategoryId,
			ParentId:   0,
			Content:    "Post without tags",
		}
		_, err := f.msgServer.CreatePost(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("tag not found", func(t *testing.T) {
		msg := &types.MsgCreatePost{
			Creator:    testCreator,
			CategoryId: cat.CategoryId,
			ParentId:   0,
			Content:    "Post with missing tag",
			Tags:       []string{"nonexistent-tag"},
		}
		_, err := f.msgServer.CreatePost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "tag not found")
	})

	t.Run("invalid tag format", func(t *testing.T) {
		msg := &types.MsgCreatePost{
			Creator:    testCreator,
			CategoryId: cat.CategoryId,
			ParentId:   0,
			Content:    "Post with bad tag",
			Tags:       []string{"UPPERCASE"},
		}
		_, err := f.msgServer.CreatePost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid tag format")
	})

	t.Run("too many tags", func(t *testing.T) {
		msg := &types.MsgCreatePost{
			Creator:    testCreator,
			CategoryId: cat.CategoryId,
			ParentId:   0,
			Content:    "Post with too many tags",
			Tags:       []string{"golang", "cosmos-sdk", "testing", "alpha", "beta", "gamma"},
		}
		_, err := f.msgServer.CreatePost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "tag limit exceeded")
	})

	t.Run("duplicate tags", func(t *testing.T) {
		msg := &types.MsgCreatePost{
			Creator:    testCreator,
			CategoryId: cat.CategoryId,
			ParentId:   0,
			Content:    "Post with duplicate tags",
			Tags:       []string{"golang", "golang"},
		}
		_, err := f.msgServer.CreatePost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate tag")
	})

	t.Run("reserved tag rejected", func(t *testing.T) {
		// Create a reserved tag
		f.createTestTag(t, "official")
		_ = f.keeper.ReservedTag.Set(f.ctx, "official", types.ReservedTag{
			Name:      "official",
			Authority: testAuthority,
		})

		msg := &types.MsgCreatePost{
			Creator:    testCreator,
			CategoryId: cat.CategoryId,
			ParentId:   0,
			Content:    "Post with reserved tag",
			Tags:       []string{"official"},
		}
		_, err := f.msgServer.CreatePost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "tag is reserved")
	})
}

func TestCreatePostContentType(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")

	tests := []struct {
		name        string
		contentType commontypes.ContentType
	}{
		{"default (unspecified)", commontypes.ContentType_CONTENT_TYPE_UNSPECIFIED},
		{"plain text", commontypes.ContentType_CONTENT_TYPE_TEXT},
		{"markdown", commontypes.ContentType_CONTENT_TYPE_MARKDOWN},
		{"gzip", commontypes.ContentType_CONTENT_TYPE_GZIP},
		{"ipfs", commontypes.ContentType_CONTENT_TYPE_IPFS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextID, _ := f.keeper.PostSeq.Peek(f.ctx)

			msg := &types.MsgCreatePost{
				Creator:     testCreator,
				CategoryId:  cat.CategoryId,
				ParentId:    0,
				Content:     "Content type test",
				ContentType: tt.contentType,
			}
			_, err := f.msgServer.CreatePost(f.ctx, msg)
			require.NoError(t, err)

			post, err := f.keeper.Post.Get(f.ctx, nextID)
			require.NoError(t, err)
			require.Equal(t, tt.contentType, post.ContentType)
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

func TestCreatePostStorageFee(t *testing.T) {
	t.Run("storage fee charged to members", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")

		// Reset bank keeper tracking
		f.bankKeeper.SendCoinsFromAccountToModuleCalls = nil
		f.bankKeeper.BurnCoinsCalls = nil

		content := "Hello World!" // 12 bytes
		msg := &types.MsgCreatePost{
			Creator:    testCreator,
			CategoryId: cat.CategoryId,
			ParentId:   0,
			Content:    content,
		}
		_, err := f.msgServer.CreatePost(f.ctx, msg)
		require.NoError(t, err)

		// Default cost_per_byte = 100 uspark/byte, total = 12 * 100 = 1200 uspark
		expectedFee := sdk.NewCoin("uspark", math.NewInt(int64(len(content))*100))
		require.GreaterOrEqual(t, len(f.bankKeeper.SendCoinsFromAccountToModuleCalls), 1)
		require.Equal(t, sdk.NewCoins(expectedFee), f.bankKeeper.SendCoinsFromAccountToModuleCalls[0].Amt)
		require.GreaterOrEqual(t, len(f.bankKeeper.BurnCoinsCalls), 1)
		require.Equal(t, sdk.NewCoins(expectedFee), f.bankKeeper.BurnCoinsCalls[0].Amt)
	})

	t.Run("cost_per_byte_exempt disables storage fee", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")

		params := types.DefaultParams()
		params.CostPerByteExempt = true
		f.keeper.Params.Set(f.ctx, params)

		f.bankKeeper.SendCoinsFromAccountToModuleCalls = nil
		f.bankKeeper.BurnCoinsCalls = nil

		msg := &types.MsgCreatePost{
			Creator:    testCreator,
			CategoryId: cat.CategoryId,
			ParentId:   0,
			Content:    "Test content",
		}
		_, err := f.msgServer.CreatePost(f.ctx, msg)
		require.NoError(t, err)

		// No storage fee should be charged (exempt)
		// Members don't pay spam_tax either, so no bank calls at all
		require.Len(t, f.bankKeeper.SendCoinsFromAccountToModuleCalls, 0)
	})

	t.Run("insufficient funds returns error", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")

		f.bankKeeper.SendCoinsFromAccountToModuleFn = func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
			return fmt.Errorf("insufficient funds")
		}

		msg := &types.MsgCreatePost{
			Creator:    testCreator,
			CategoryId: cat.CategoryId,
			ParentId:   0,
			Content:    "Test content",
		}
		_, err := f.msgServer.CreatePost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to charge storage fee")
	})
}
