package keeper_test

import (
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

func setupMsgServerForDelete(t testing.TB) (keeper.Keeper, types.MsgServer, sdk.Context) {
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec("sprkdrm")

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)

	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	// Use gov module account as authority
	authority := authtypes.NewModuleAddress(types.GovModuleName)

	bankKeeper := &mockBankKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		bankKeeper,
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return k, keeper.NewMsgServerImpl(k), ctx
}

func TestDeletePost(t *testing.T) {
	k, msgServer, ctx := setupMsgServerForDelete(t)

	creator1 := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	creator2 := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	// Create a post first
	createMsg := &types.MsgCreatePost{
		Creator: creator1,
		Title:   "Test Post",
		Body:    "This is a test post body",
	}
	createResp, err := msgServer.CreatePost(ctx, createMsg)
	require.NoError(t, err)
	postID := createResp.Id

	tests := []struct {
		name        string
		msg         *types.MsgDeletePost
		expectError bool
		errContains string
	}{
		{
			name: "successful post deletion",
			msg: &types.MsgDeletePost{
				Creator: creator1,
				Id:      postID,
			},
			expectError: false,
		},
		{
			name: "delete non-existent post",
			msg: &types.MsgDeletePost{
				Creator: creator1,
				Id:      99999,
			},
			expectError: true,
			errContains: "doesn't exist",
		},
		{
			name: "delete with wrong creator",
			msg: &types.MsgDeletePost{
				Creator: creator2,
				Id:      postID,
			},
			expectError: true,
			errContains: "incorrect owner",
		},
		{
			name: "invalid creator address",
			msg: &types.MsgDeletePost{
				Creator: "invalid-address",
				Id:      postID,
			},
			expectError: true,
			errContains: "invalid creator address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For tests other than successful deletion, create a fresh post
			var testPostID uint64
			if tt.name != "successful post deletion" && tt.name != "delete non-existent post" {
				freshCreateMsg := &types.MsgCreatePost{
					Creator: creator1,
					Title:   "Fresh Test Post",
					Body:    "Fresh test post body",
				}
				freshResp, err := msgServer.CreatePost(ctx, freshCreateMsg)
				require.NoError(t, err)
				testPostID = freshResp.Id
				tt.msg.Id = testPostID
			}

			_, err := msgServer.DeletePost(ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)

				// Verify post still exists if it should
				if tt.name == "delete with wrong creator" || tt.name == "invalid creator address" {
					_, found := k.GetPost(ctx, testPostID)
					require.True(t, found, "post should still exist after failed deletion")
				}
			} else {
				require.NoError(t, err)

				// Verify post was actually deleted
				_, found := k.GetPost(ctx, tt.msg.Id)
				require.False(t, found, "post should be deleted")
			}
		})
	}
}

func TestDeletePostOwnership(t *testing.T) {
	k, msgServer, ctx := setupMsgServerForDelete(t)

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

	// Creator 1 should not be able to delete Creator 2's post
	deleteMsg := &types.MsgDeletePost{
		Creator: creator1,
		Id:      resp2.Id,
	}
	_, err = msgServer.DeletePost(ctx, deleteMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "incorrect owner")

	// Verify post still exists
	post, found := k.GetPost(ctx, resp2.Id)
	require.True(t, found)
	require.Equal(t, creator2, post.Creator)

	// Creator 2 should be able to delete their own post
	deleteMsg2 := &types.MsgDeletePost{
		Creator: creator2,
		Id:      resp2.Id,
	}
	_, err = msgServer.DeletePost(ctx, deleteMsg2)
	require.NoError(t, err)

	// Verify deletion
	_, found = k.GetPost(ctx, resp2.Id)
	require.False(t, found)

	// Creator 1's post should still exist
	post1, found := k.GetPost(ctx, resp1.Id)
	require.True(t, found)
	require.Equal(t, creator1, post1.Creator)
}

func TestDeleteAlreadyDeletedPost(t *testing.T) {
	_, msgServer, ctx := setupMsgServerForDelete(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create and delete a post
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

	// Try to delete again
	_, err = msgServer.DeletePost(ctx, deleteMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "doesn't exist")
}
