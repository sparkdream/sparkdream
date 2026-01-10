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

func setupMsgServer(t testing.TB) (keeper.Keeper, types.MsgServer, sdk.Context) {
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

func TestCreatePost(t *testing.T) {
	k, msgServer, ctx := setupMsgServer(t)

	tests := []struct {
		name        string
		msg         *types.MsgCreatePost
		expectError bool
		errContains string
	}{
		{
			name: "successful post creation",
			msg: &types.MsgCreatePost{
				Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
				Title:   "Test Post",
				Body:    "This is a test post body",
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgCreatePost{
				Creator: "invalid-address",
				Title:   "Invalid Address Post",
				Body:    "This should fail",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "empty title",
			msg: &types.MsgCreatePost{
				Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
				Title:   "",
				Body:    "Post with empty title",
			},
			expectError: true,
			errContains: "title cannot be empty",
		},
		{
			name: "empty body",
			msg: &types.MsgCreatePost{
				Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
				Title:   "Empty Body Post",
				Body:    "",
			},
			expectError: true,
			errContains: "body cannot be empty",
		},
		{
			name: "title exceeds max length",
			msg: &types.MsgCreatePost{
				Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
				Title:   string(bytes.Repeat([]byte("a"), 201)),
				Body:    "Valid body",
			},
			expectError: true,
			errContains: "title exceeds maximum length",
		},
		{
			name: "body exceeds max length",
			msg: &types.MsgCreatePost{
				Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
				Title:   "Valid Title",
				Body:    string(bytes.Repeat([]byte("a"), 10001)),
			},
			expectError: true,
			errContains: "body exceeds maximum length",
		},
		{
			name: "title at max length (200 chars)",
			msg: &types.MsgCreatePost{
				Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
				Title:   string(bytes.Repeat([]byte("a"), 200)),
				Body:    "Valid body",
			},
			expectError: false,
		},
		{
			name: "body at max length (10000 chars)",
			msg: &types.MsgCreatePost{
				Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
				Title:   "Valid Title",
				Body:    string(bytes.Repeat([]byte("a"), 10000)),
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := msgServer.CreatePost(ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)

				// Verify post was actually created
				post, found := k.GetPost(ctx, resp.Id)
				require.True(t, found)
				require.Equal(t, tt.msg.Creator, post.Creator)
				require.Equal(t, tt.msg.Title, post.Title)
				require.Equal(t, tt.msg.Body, post.Body)
				require.Equal(t, resp.Id, post.Id)
			}
		})
	}
}

func TestCreatePostIDIncrement(t *testing.T) {
	_, msgServer, ctx := setupMsgServer(t)

	validMsg := &types.MsgCreatePost{
		Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		Title:   "Test Post",
		Body:    "This is a test post body",
	}

	// Create multiple posts and verify IDs increment
	lastID := uint64(0)
	for i := 0; i < 5; i++ {
		resp, err := msgServer.CreatePost(ctx, validMsg)
		require.NoError(t, err)
		if i > 0 {
			require.Greater(t, resp.Id, lastID)
		}
		lastID = resp.Id
	}
}
