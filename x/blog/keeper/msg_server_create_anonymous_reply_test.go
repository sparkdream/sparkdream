package keeper_test

import (
	"context"
	"fmt"
	"strings"
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

type mockVoteKeeperReply struct {
	VerifyAnonymousActionProofFn func(ctx context.Context, proof, nullifier, merkleRoot []byte, minTrustLevel uint32) error
}

func (m *mockVoteKeeperReply) VerifyAnonymousActionProof(ctx context.Context, proof, nullifier, merkleRoot []byte, minTrustLevel uint32) error {
	if m.VerifyAnonymousActionProofFn != nil {
		return m.VerifyAnonymousActionProofFn(ctx, proof, nullifier, merkleRoot, minTrustLevel)
	}
	return nil // dev mode: accept all
}

func setupAnonReplyMsgServer(t testing.TB) (keeper.Keeper, types.MsgServer, sdk.Context, *mockBankKeeper, *mockRepKeeper, *mockVoteKeeperReply) {
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec("sprkdrm")
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	authority := authtypes.NewModuleAddress(types.GovModuleName)
	bankKeeper := &mockBankKeeper{}
	repKeeper := &mockRepKeeper{}
	voteKeeper := &mockVoteKeeperReply{}
	k := keeper.NewKeeper(storeService, encCfg.Codec, addressCodec, authority, bankKeeper, nil, repKeeper)
	k.SetVoteKeeper(voteKeeper)
	params := types.DefaultParams()
	params.MaxPostsPerDay = 100
	params.MaxRepliesPerDay = 100
	params.AnonymousPostingEnabled = true
	params.AnonymousMinTrustLevel = 1
	if err := k.Params.Set(ctx, params); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}
	return k, keeper.NewMsgServerImpl(k), ctx, bankKeeper, repKeeper, voteKeeper
}

// createTestPost is a helper that creates a regular post for reply tests.
func createTestPost(t testing.TB, k keeper.Keeper, ctx sdk.Context, repliesEnabled bool, minReplyTrustLevel int32) uint64 {
	t.Helper()
	post := types.Post{
		Creator:            "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		Title:              "Test Post for Replies",
		Body:               "A post body to test replies against",
		RepliesEnabled:     repliesEnabled,
		MinReplyTrustLevel: minReplyTrustLevel,
		Status:             types.PostStatus_POST_STATUS_ACTIVE,
	}
	return k.AppendPost(ctx, post)
}

func TestCreateAnonymousReply(t *testing.T) {
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	tests := []struct {
		name        string
		setup       func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperReply, rk *mockRepKeeper, ms types.MsgServer) uint64
		buildMsg    func(postId uint64) *types.MsgCreateAnonymousReply
		useNoVote   bool // if true, use setupMsgServer (no VoteKeeper)
		expectError bool
		errContains string
	}{
		{
			name: "successful anonymous reply",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperReply, rk *mockRepKeeper, ms types.MsgServer) uint64 {
				return createTestPost(t, *k, ctx, true, 0)
			},
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        postId,
					Body:          "This is an anonymous reply",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("fake-nullifier-reply"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				}
			},
			expectError: false,
		},
		{
			name: "anonymous posting disabled",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperReply, rk *mockRepKeeper, ms types.MsgServer) uint64 {
				postId := createTestPost(t, *k, ctx, true, 0)
				params, _ := k.Params.Get(ctx)
				params.AnonymousPostingEnabled = false
				k.Params.Set(ctx, params)
				return postId
			},
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        postId,
					Body:          "Should fail",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("fake-nullifier"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				}
			},
			expectError: true,
			errContains: "anonymous posting is not enabled",
		},
		{
			name:      "vote keeper not available",
			useNoVote: true,
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        0,
					Body:          "Should fail without vote module",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("fake-nullifier"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				}
			},
			expectError: true,
			errContains: "vote module not available",
		},
		{
			name: "post not found",
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        99999,
					Body:          "Post does not exist",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("fake-nullifier"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				}
			},
			expectError: true,
			errContains: "post 99999 not found",
		},
		{
			name: "post not active",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperReply, rk *mockRepKeeper, ms types.MsgServer) uint64 {
				post := types.Post{
					Creator:        submitter,
					Title:          "Deleted Post",
					Body:           "This post is deleted",
					RepliesEnabled: true,
					Status:         types.PostStatus_POST_STATUS_DELETED,
				}
				return k.AppendPost(ctx, post)
			},
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        postId,
					Body:          "Reply to deleted post",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("fake-nullifier-deleted"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				}
			},
			expectError: true,
			errContains: "has been deleted",
		},
		{
			name: "replies disabled on post",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperReply, rk *mockRepKeeper, ms types.MsgServer) uint64 {
				return createTestPost(t, *k, ctx, false, 0)
			},
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        postId,
					Body:          "Reply to no-replies post",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("fake-nullifier-no-reply"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				}
			},
			expectError: true,
			errContains: "replies are disabled",
		},
		{
			name: "empty body",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperReply, rk *mockRepKeeper, ms types.MsgServer) uint64 {
				return createTestPost(t, *k, ctx, true, 0)
			},
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        postId,
					Body:          "",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("fake-nullifier-empty"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				}
			},
			expectError: true,
			errContains: "body cannot be empty",
		},
		{
			name: "body too long",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperReply, rk *mockRepKeeper, ms types.MsgServer) uint64 {
				return createTestPost(t, *k, ctx, true, 0)
			},
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        postId,
					Body:          strings.Repeat("a", 2001), // DefaultMaxReplyLength = 2000
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("fake-nullifier-long"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				}
			},
			expectError: true,
			errContains: "body too long",
		},
		{
			name: "min trust level below required (max of params and post min)",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperReply, rk *mockRepKeeper, ms types.MsgServer) uint64 {
				// Create post with MinReplyTrustLevel=3 (higher than params=1)
				return createTestPost(t, *k, ctx, true, 3)
			},
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        postId,
					Body:          "Low trust reply",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("fake-nullifier-low-trust"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 2, // post requires 3
				}
			},
			expectError: true,
			errContains: "min_trust_level 2 below required 3",
		},
		{
			name: "invalid merkle root",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperReply, rk *mockRepKeeper, ms types.MsgServer) uint64 {
				return createTestPost(t, *k, ctx, true, 0)
			},
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        postId,
					Body:          "Wrong root reply",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("fake-nullifier-wrong-root"),
					MerkleRoot:    []byte("wrong-merkle-root"),
					MinTrustLevel: 1,
				}
			},
			expectError: true,
			errContains: "stale or invalid merkle root",
		},
		{
			name: "nullifier already used for same post",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperReply, rk *mockRepKeeper, ms types.MsgServer) uint64 {
				postId := createTestPost(t, *k, ctx, true, 0)
				// Submit a reply first with the same nullifier
				_, err := ms.CreateAnonymousReply(ctx, &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        postId,
					Body:          "First anonymous reply",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("duplicate-reply-nullifier"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				})
				require.NoError(t, err)
				return postId
			},
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        postId,
					Body:          "Duplicate nullifier reply",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("duplicate-reply-nullifier"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				}
			},
			expectError: true,
			errContains: "nullifier already used",
		},
		{
			name: "ZK proof verification failure",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperReply, rk *mockRepKeeper, ms types.MsgServer) uint64 {
				postId := createTestPost(t, *k, ctx, true, 0)
				vk.VerifyAnonymousActionProofFn = func(ctx context.Context, proof, nullifier, merkleRoot []byte, minTrustLevel uint32) error {
					return fmt.Errorf("proof verification failed")
				}
				return postId
			},
			buildMsg: func(postId uint64) *types.MsgCreateAnonymousReply {
				return &types.MsgCreateAnonymousReply{
					Submitter:     submitter,
					PostId:        postId,
					Body:          "Bad proof reply",
					Proof:         []byte("invalid-proof"),
					Nullifier:     []byte("fake-nullifier-bad-proof"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				}
			},
			expectError: true,
			errContains: "proof verification failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				k   keeper.Keeper
				ms  types.MsgServer
				ctx sdk.Context
				vk  *mockVoteKeeperReply
				rk  *mockRepKeeper
			)

			postId := uint64(0)

			if tt.useNoVote {
				// Use setupMsgServer which does NOT wire a VoteKeeper
				var bk *mockBankKeeper
				k, ms, ctx, bk = setupMsgServer(t)
				_ = bk
				// Enable anonymous posting so the check reaches voteKeeper nil guard
				params, _ := k.Params.Get(ctx)
				params.AnonymousPostingEnabled = true
				params.AnonymousMinTrustLevel = 1
				params.MaxRepliesPerDay = 100
				k.Params.Set(ctx, params)
			} else {
				k, ms, ctx, _, rk, vk = setupAnonReplyMsgServer(t)
			}

			if tt.setup != nil {
				postId = tt.setup(&k, ctx, vk, rk, ms)
			}

			msg := tt.buildMsg(postId)
			resp, err := ms.CreateAnonymousReply(ctx, msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify reply was created
				reply, found := k.GetReply(ctx, resp.Id)
				require.True(t, found)
				require.Equal(t, msg.PostId, reply.PostId)
				require.Equal(t, msg.Body, reply.Body)
				require.Equal(t, types.ReplyStatus_REPLY_STATUS_ACTIVE, reply.Status)
			}
		})
	}
}

func TestCreateAnonymousReplyCreatorIsModuleAddress(t *testing.T) {
	k, ms, ctx, _, _, _ := setupAnonReplyMsgServer(t)
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()

	postId := createTestPost(t, k, ctx, true, 0)

	resp, err := ms.CreateAnonymousReply(ctx, &types.MsgCreateAnonymousReply{
		Submitter:     submitter,
		PostId:        postId,
		Body:          "Creator should be module address",
		Proof:         []byte("fake-proof"),
		Nullifier:     []byte("nullifier-creator-reply"),
		MerkleRoot:    []byte("mock-trust-tree-root"),
		MinTrustLevel: 1,
	})
	require.NoError(t, err)

	reply, found := k.GetReply(ctx, resp.Id)
	require.True(t, found)
	require.Equal(t, moduleAddr, reply.Creator, "anonymous reply creator should be the blog module address")
	require.NotEqual(t, submitter, reply.Creator, "anonymous reply creator must not be the submitter")
}

func TestCreateAnonymousReplyMetadataStored(t *testing.T) {
	k, ms, ctx, _, _, _ := setupAnonReplyMsgServer(t)
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	nullifier := []byte("nullifier-meta-reply")
	merkleRoot := []byte("mock-trust-tree-root")

	postId := createTestPost(t, k, ctx, true, 0)

	resp, err := ms.CreateAnonymousReply(ctx, &types.MsgCreateAnonymousReply{
		Submitter:     submitter,
		PostId:        postId,
		Body:          "Metadata should be stored for reply",
		Proof:         []byte("fake-proof"),
		Nullifier:     nullifier,
		MerkleRoot:    merkleRoot,
		MinTrustLevel: 1,
	})
	require.NoError(t, err)

	meta, found := k.GetAnonymousReplyMeta(ctx, resp.Id)
	require.True(t, found, "anonymous reply metadata should be stored")
	require.Equal(t, resp.Id, meta.ContentId)
	require.Equal(t, nullifier, meta.Nullifier)
	require.Equal(t, merkleRoot, meta.MerkleRoot)
	require.Equal(t, uint32(1), meta.ProvenTrustLevel)
}

func TestCreateAnonymousReplyIncrementPostReplyCount(t *testing.T) {
	k, ms, ctx, _, _, _ := setupAnonReplyMsgServer(t)
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	postId := createTestPost(t, k, ctx, true, 0)

	// Verify initial reply count is 0
	post, found := k.GetPost(ctx, postId)
	require.True(t, found)
	require.Equal(t, uint64(0), post.ReplyCount)

	// Create first anonymous reply
	_, err := ms.CreateAnonymousReply(ctx, &types.MsgCreateAnonymousReply{
		Submitter:     submitter,
		PostId:        postId,
		Body:          "First reply",
		Proof:         []byte("fake-proof"),
		Nullifier:     []byte("nullifier-count-1"),
		MerkleRoot:    []byte("mock-trust-tree-root"),
		MinTrustLevel: 1,
	})
	require.NoError(t, err)

	post, found = k.GetPost(ctx, postId)
	require.True(t, found)
	require.Equal(t, uint64(1), post.ReplyCount, "reply count should be 1 after first reply")

	// Create second anonymous reply
	_, err = ms.CreateAnonymousReply(ctx, &types.MsgCreateAnonymousReply{
		Submitter:     submitter,
		PostId:        postId,
		Body:          "Second reply",
		Proof:         []byte("fake-proof"),
		Nullifier:     []byte("nullifier-count-2"),
		MerkleRoot:    []byte("mock-trust-tree-root"),
		MinTrustLevel: 1,
	})
	require.NoError(t, err)

	post, found = k.GetPost(ctx, postId)
	require.True(t, found)
	require.Equal(t, uint64(2), post.ReplyCount, "reply count should be 2 after second reply")
}
