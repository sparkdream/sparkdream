package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestDownvotePost(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	tests := []struct {
		name        string
		msg         *types.MsgDownvotePost
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful downvote",
			msg: &types.MsgDownvotePost{
				Creator: testCreator2,
				PostId:  post.PostId,
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgDownvotePost{
				Creator: "invalid-address",
				PostId:  post.PostId,
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "reactions disabled",
			msg: &types.MsgDownvotePost{
				Creator: testCreator2,
				PostId:  post.PostId,
			},
			setup: func() {
				params := types.DefaultParams()
				params.ReactionsEnabled = false
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "reactions are disabled",
		},
		{
			name: "post not found",
			msg: &types.MsgDownvotePost{
				Creator: testCreator2,
				PostId:  9999,
			},
			expectError: true,
			errContains: "post not found",
		},
		{
			name: "post hidden",
			msg: &types.MsgDownvotePost{
				Creator: testCreator2,
				PostId:  post.PostId,
			},
			setup: func() {
				p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
				p.Status = types.PostStatus_POST_STATUS_HIDDEN
				_ = f.keeper.Post.Set(f.ctx, post.PostId, p)
			},
			expectError: true,
			errContains: "hidden",
		},
		{
			name: "cannot downvote own post",
			msg: &types.MsgDownvotePost{
				Creator: testCreator, // Same as post author
				PostId:  post.PostId,
			},
			expectError: true,
			errContains: "cannot vote on your own post",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset state
			_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
			p.Status = types.PostStatus_POST_STATUS_ACTIVE
			p.Author = testCreator
			_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.DownvotePost(f.ctx, tt.msg)

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

func TestDownvotePostDepositBurn(t *testing.T) {
	f := initFixture(t)

	// Track bank calls
	var burnedCoins sdk.Coins
	f.bankKeeper.SendCoinsFromAccountToModuleFn = func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		return nil
	}
	f.bankKeeper.BurnCoinsFn = func(ctx context.Context, moduleName string, amt sdk.Coins) error {
		burnedCoins = amt
		return nil
	}

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Downvote
	_, err := f.msgServer.DownvotePost(f.ctx, &types.MsgDownvotePost{
		Creator: testCreator2,
		PostId:  post.PostId,
	})
	require.NoError(t, err)

	// Verify deposit was burned
	require.NotEmpty(t, burnedCoins)
}

func TestUpvotePost(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Upvote
	resp, err := f.msgServer.UpvotePost(f.ctx, &types.MsgUpvotePost{
		Creator: testCreator2,
		PostId:  post.PostId,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestUpvotePostReactionsDisabled(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Disable reactions
	params := types.DefaultParams()
	params.ReactionsEnabled = false
	_ = f.keeper.Params.Set(f.ctx, params)

	// Try to upvote
	_, err := f.msgServer.UpvotePost(f.ctx, &types.MsgUpvotePost{
		Creator: testCreator2,
		PostId:  post.PostId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "reactions are disabled")
}

func TestVoteOnOwnPost(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Try to upvote own post
	_, err := f.msgServer.UpvotePost(f.ctx, &types.MsgUpvotePost{
		Creator: testCreator, // Same as post author
		PostId:  post.PostId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot vote on your own post")

	// Try to downvote own post
	_, err = f.msgServer.DownvotePost(f.ctx, &types.MsgDownvotePost{
		Creator: testCreator,
		PostId:  post.PostId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot vote on your own post")
}
