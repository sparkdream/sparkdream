package keeper_test

import (
	"bytes"
	"testing"

	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	module "sparkdream/x/blog/module"
	"sparkdream/x/blog/types"
)

func setupMsgServerForUpdate(t testing.TB) (keeper.Keeper, types.MsgServer, sdk.Context) {
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec("sprkdrm")

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)

	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	// Use gov module account as authority
	authority := authtypes.NewModuleAddress(types.GovModuleName)

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return k, keeper.NewMsgServerImpl(k), ctx
}

func TestUpdatePost(t *testing.T) {
	k, msgServer, ctx := setupMsgServerForUpdate(t)

	creator1 := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	creator2 := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	// Create a post first
	createMsg := &types.MsgCreatePost{
		Creator: creator1,
		Title:   "Original Title",
		Body:    "Original body content",
	}
	createResp, err := msgServer.CreatePost(ctx, createMsg)
	require.NoError(t, err)
	postID := createResp.Id

	tests := []struct {
		name        string
		msg         *types.MsgUpdatePost
		expectError bool
		errContains string
	}{
		{
			name: "successful post update",
			msg: &types.MsgUpdatePost{
				Creator: creator1,
				Id:      postID,
				Title:   "Updated Title",
				Body:    "Updated body content",
			},
			expectError: false,
		},
		{
			name: "update non-existent post",
			msg: &types.MsgUpdatePost{
				Creator: creator1,
				Id:      99999,
				Title:   "Title",
				Body:    "Body",
			},
			expectError: true,
			errContains: "doesn't exist",
		},
		{
			name: "update with wrong creator",
			msg: &types.MsgUpdatePost{
				Creator: creator2,
				Id:      postID,
				Title:   "Unauthorized Title",
				Body:    "Unauthorized body",
			},
			expectError: true,
			errContains: "incorrect owner",
		},
		{
			name: "invalid creator address",
			msg: &types.MsgUpdatePost{
				Creator: "invalid-address",
				Id:      postID,
				Title:   "Title",
				Body:    "Body",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "empty title",
			msg: &types.MsgUpdatePost{
				Creator: creator1,
				Id:      postID,
				Title:   "",
				Body:    "Valid body",
			},
			expectError: true,
			errContains: "title cannot be empty",
		},
		{
			name: "empty body",
			msg: &types.MsgUpdatePost{
				Creator: creator1,
				Id:      postID,
				Title:   "Valid title",
				Body:    "",
			},
			expectError: true,
			errContains: "body cannot be empty",
		},
		{
			name: "title exceeds max length",
			msg: &types.MsgUpdatePost{
				Creator: creator1,
				Id:      postID,
				Title:   string(bytes.Repeat([]byte("a"), 201)),
				Body:    "Valid body",
			},
			expectError: true,
			errContains: "title exceeds maximum length",
		},
		{
			name: "body exceeds max length",
			msg: &types.MsgUpdatePost{
				Creator: creator1,
				Id:      postID,
				Title:   "Valid title",
				Body:    string(bytes.Repeat([]byte("a"), 10001)),
			},
			expectError: true,
			errContains: "body exceeds maximum length",
		},
		{
			name: "title at max length (200 chars)",
			msg: &types.MsgUpdatePost{
				Creator: creator1,
				Id:      postID,
				Title:   string(bytes.Repeat([]byte("a"), 200)),
				Body:    "Valid body",
			},
			expectError: false,
		},
		{
			name: "body at max length (10000 chars)",
			msg: &types.MsgUpdatePost{
				Creator: creator1,
				Id:      postID,
				Title:   "Valid title",
				Body:    string(bytes.Repeat([]byte("a"), 10000)),
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For each test, create a fresh post to avoid interference
			freshCreateMsg := &types.MsgCreatePost{
				Creator: creator1,
				Title:   "Fresh Original Title",
				Body:    "Fresh original body",
			}
			freshResp, err := msgServer.CreatePost(ctx, freshCreateMsg)
			require.NoError(t, err)
			testPostID := freshResp.Id

			// Update the message ID unless it's specifically testing non-existent post
			if tt.name != "update non-existent post" {
				tt.msg.Id = testPostID
			}

			// Get original post for comparison
			originalPost, found := k.GetPost(ctx, testPostID)
			require.True(t, found)

			_, err = msgServer.UpdatePost(ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)

				// Verify post wasn't modified on error
				if tt.name != "update non-existent post" {
					post, found := k.GetPost(ctx, testPostID)
					require.True(t, found)
					require.Equal(t, originalPost.Title, post.Title, "title should not change on error")
					require.Equal(t, originalPost.Body, post.Body, "body should not change on error")
				}
			} else {
				require.NoError(t, err)

				// Verify post was actually updated
				post, found := k.GetPost(ctx, testPostID)
				require.True(t, found)
				require.Equal(t, tt.msg.Creator, post.Creator)
				require.Equal(t, tt.msg.Title, post.Title)
				require.Equal(t, tt.msg.Body, post.Body)
				require.Equal(t, testPostID, post.Id)
			}
		})
	}
}

func TestUpdatePostOwnership(t *testing.T) {
	k, msgServer, ctx := setupMsgServerForUpdate(t)

	creator1 := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	creator2 := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	// Create posts by different creators
	createMsg1 := &types.MsgCreatePost{
		Creator: creator1,
		Title:   "Creator 1 Post",
		Body:    "Post by creator 1",
	}
	resp1, err := msgServer.CreatePost(ctx, createMsg1)
	require.NoError(t, err)

	createMsg2 := &types.MsgCreatePost{
		Creator: creator2,
		Title:   "Creator 2 Post",
		Body:    "Post by creator 2",
	}
	resp2, err := msgServer.CreatePost(ctx, createMsg2)
	require.NoError(t, err)

	// Creator 1 should not be able to update Creator 2's post
	updateMsg := &types.MsgUpdatePost{
		Creator: creator1,
		Id:      resp2.Id,
		Title:   "Hacked Title",
		Body:    "Hacked Body",
	}
	_, err = msgServer.UpdatePost(ctx, updateMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "incorrect owner")

	// Verify post wasn't modified
	post, found := k.GetPost(ctx, resp2.Id)
	require.True(t, found)
	require.Equal(t, creator2, post.Creator)
	require.Equal(t, "Creator 2 Post", post.Title)
	require.Equal(t, "Post by creator 2", post.Body)

	// Creator 2 should be able to update their own post
	updateMsg2 := &types.MsgUpdatePost{
		Creator: creator2,
		Id:      resp2.Id,
		Title:   "Updated Creator 2 Post",
		Body:    "Updated by creator 2",
	}
	_, err = msgServer.UpdatePost(ctx, updateMsg2)
	require.NoError(t, err)

	// Verify update
	post, found = k.GetPost(ctx, resp2.Id)
	require.True(t, found)
	require.Equal(t, creator2, post.Creator)
	require.Equal(t, "Updated Creator 2 Post", post.Title)
	require.Equal(t, "Updated by creator 2", post.Body)

	// Creator 1's post should be unchanged
	post1, found := k.GetPost(ctx, resp1.Id)
	require.True(t, found)
	require.Equal(t, creator1, post1.Creator)
	require.Equal(t, "Creator 1 Post", post1.Title)
	require.Equal(t, "Post by creator 1", post1.Body)
}

func TestUpdatePostMultipleTimes(t *testing.T) {
	k, msgServer, ctx := setupMsgServerForUpdate(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create a post
	createMsg := &types.MsgCreatePost{
		Creator: creator,
		Title:   "Version 1",
		Body:    "Body version 1",
	}
	createResp, err := msgServer.CreatePost(ctx, createMsg)
	require.NoError(t, err)
	postID := createResp.Id

	// Update multiple times
	versions := []struct {
		title string
		body  string
	}{
		{"Version 2", "Body version 2"},
		{"Version 3", "Body version 3"},
		{"Version 4", "Body version 4"},
		{"Final Version", "Final body content"},
	}

	for _, v := range versions {
		updateMsg := &types.MsgUpdatePost{
			Creator: creator,
			Id:      postID,
			Title:   v.title,
			Body:    v.body,
		}
		_, err := msgServer.UpdatePost(ctx, updateMsg)
		require.NoError(t, err)

		// Verify each update
		post, found := k.GetPost(ctx, postID)
		require.True(t, found)
		require.Equal(t, v.title, post.Title)
		require.Equal(t, v.body, post.Body)
	}

	// Final verification
	finalPost, found := k.GetPost(ctx, postID)
	require.True(t, found)
	require.Equal(t, "Final Version", finalPost.Title)
	require.Equal(t, "Final body content", finalPost.Body)
	require.Equal(t, creator, finalPost.Creator)
	require.Equal(t, postID, finalPost.Id)
}

func TestUpdateDeletedPost(t *testing.T) {
	_, msgServer, ctx := setupMsgServerForUpdate(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create a post
	createMsg := &types.MsgCreatePost{
		Creator: creator,
		Title:   "Test Post",
		Body:    "This post will be deleted",
	}
	createResp, err := msgServer.CreatePost(ctx, createMsg)
	require.NoError(t, err)
	postID := createResp.Id

	// Delete the post
	deleteMsg := &types.MsgDeletePost{
		Creator: creator,
		Id:      postID,
	}
	_, err = msgServer.DeletePost(ctx, deleteMsg)
	require.NoError(t, err)

	// Try to update the deleted post
	updateMsg := &types.MsgUpdatePost{
		Creator: creator,
		Id:      postID,
		Title:   "Updated Title",
		Body:    "Updated Body",
	}
	_, err = msgServer.UpdatePost(ctx, updateMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "doesn't exist")
}
