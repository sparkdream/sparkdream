package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestAnonymousReact(t *testing.T) {
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	tests := []struct {
		name        string
		setup       func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer)
		msg         *types.MsgAnonymousReact
		useNoVote   bool
		expectError bool
		errContains string
	}{
		{
			name: "successful anonymous reaction on post",
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0, // will be set in setup
				ReplyId:       0,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier-react"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				// Create a post to react on
				k.SetPost(ctx, types.Post{
					Id:      0,
					Creator: submitter,
					Title:   "Test Post",
					Body:    "Test body",
					Status:  types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.SetPostCount(ctx, 1)
			},
			expectError: false,
		},
		{
			name: "successful anonymous reaction on reply",
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReplyId:       0, // will be set in setup
				ReactionType:  types.ReactionType_REACTION_TYPE_INSIGHTFUL,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier-react-reply"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:      0,
					Creator: submitter,
					Title:   "Test Post",
					Body:    "Test body",
					Status:  types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.SetPostCount(ctx, 1)
				k.SetReply(ctx, types.Reply{
					Id:     0,
					PostId: 0,
					Body:   "Test reply",
					Status: types.ReplyStatus_REPLY_STATUS_ACTIVE,
				})
				k.SetReplyCount(ctx, 1)
			},
			expectError: false,
		},
		{
			name: "anonymous posting disabled",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				params, _ := k.Params.Get(ctx)
				params.AnonymousPostingEnabled = false
				k.Params.Set(ctx, params)
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "anonymous posting is not enabled",
		},
		{
			name:      "vote keeper not available",
			useNoVote: true,
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "vote module not available",
		},
		{
			name: "unspecified reaction type",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:     0,
					Status: types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.SetPostCount(ctx, 1)
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReactionType:  types.ReactionType_REACTION_TYPE_UNSPECIFIED,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "reaction type must be specified",
		},
		{
			name: "post not found",
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        9999,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "not found",
		},
		{
			name: "post deleted",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:     0,
					Status: types.PostStatus_POST_STATUS_DELETED,
				})
				k.SetPostCount(ctx, 1)
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "has been deleted",
		},
		{
			name: "post hidden",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:     0,
					Status: types.PostStatus_POST_STATUS_HIDDEN,
				})
				k.SetPostCount(ctx, 1)
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "is hidden",
		},
		{
			name: "reply not found",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:     0,
					Status: types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.SetPostCount(ctx, 1)
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReplyId:       9999,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "not found",
		},
		{
			name: "reply belongs to different post",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:     0,
					Status: types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.SetPostCount(ctx, 1)
				k.SetReply(ctx, types.Reply{
					Id:     1,
					PostId: 99, // different post
					Status: types.ReplyStatus_REPLY_STATUS_ACTIVE,
				})
				k.SetReplyCount(ctx, 2)
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReplyId:       1,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "reply does not belong to this post",
		},
		{
			name: "reply deleted",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:     0,
					Status: types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.SetPostCount(ctx, 1)
				k.SetReply(ctx, types.Reply{
					Id:     1,
					PostId: 0,
					Status: types.ReplyStatus_REPLY_STATUS_DELETED,
				})
				k.SetReplyCount(ctx, 2)
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReplyId:       1,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "has been deleted",
		},
		{
			name: "reply hidden",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:     0,
					Status: types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.SetPostCount(ctx, 1)
				k.SetReply(ctx, types.Reply{
					Id:     1,
					PostId: 0,
					Status: types.ReplyStatus_REPLY_STATUS_HIDDEN,
				})
				k.SetReplyCount(ctx, 2)
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReplyId:       1,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "is hidden",
		},
		{
			name: "min trust level below required",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:     0,
					Status: types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.SetPostCount(ctx, 1)
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 0, // below required 1
			},
			expectError: true,
			errContains: "min_trust_level 0 below required 1",
		},
		{
			name: "stale merkle root",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:     0,
					Status: types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.SetPostCount(ctx, 1)
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("wrong-merkle-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "stale or invalid merkle root",
		},
		{
			name: "nullifier already used",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:     0,
					Status: types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.SetPostCount(ctx, 1)
				// First, do a successful anon react to use the nullifier
				_, err := ms.AnonymousReact(ctx, &types.MsgAnonymousReact{
					Submitter:     submitter,
					PostId:        0,
					ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("duplicate-nullifier"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				})
				if err != nil {
					t.Fatalf("setup: first react failed: %v", err)
				}
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("duplicate-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "nullifier already used",
		},
		{
			name: "ZK proof verification failure",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				k.SetPost(ctx, types.Post{
					Id:     0,
					Status: types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.SetPostCount(ctx, 1)
				vk.VerifyAnonymousActionProofFn = func(ctx context.Context, proof, nullifier, merkleRoot []byte, minTrustLevel uint32) error {
					return fmt.Errorf("proof verification failed")
				}
			},
			msg: &types.MsgAnonymousReact{
				Submitter:     submitter,
				PostId:        0,
				ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
				Proof:         []byte("invalid-proof"),
				Nullifier:     []byte("fake-nullifier-bad-proof"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
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
				vk  *mockVoteKeeperPost
				rk  *mockRepKeeper
			)

			if tt.useNoVote {
				var bk *mockBankKeeper
				k, ms, ctx, bk = setupMsgServer(t)
				_ = bk
				params, _ := k.Params.Get(ctx)
				params.AnonymousPostingEnabled = true
				params.AnonymousMinTrustLevel = 1
				k.Params.Set(ctx, params)
			} else {
				k, ms, ctx, _, rk, vk = setupAnonPostMsgServer(t)
			}

			if tt.setup != nil {
				tt.setup(&k, ctx, vk, rk, ms)
			}

			resp, err := ms.AnonymousReact(ctx, tt.msg)

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

func TestAnonymousReactCountsIncremented(t *testing.T) {
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	k, ms, ctx, _, _, _ := setupAnonPostMsgServer(t)

	// Create a post
	k.SetPost(ctx, types.Post{
		Id:     0,
		Status: types.PostStatus_POST_STATUS_ACTIVE,
	})
	k.SetPostCount(ctx, 1)

	// React with LIKE
	_, err := ms.AnonymousReact(ctx, &types.MsgAnonymousReact{
		Submitter:     submitter,
		PostId:        0,
		ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
		Proof:         []byte("fake-proof"),
		Nullifier:     []byte("nullifier-like-1"),
		MerkleRoot:    []byte("mock-trust-tree-root"),
		MinTrustLevel: 1,
	})
	require.NoError(t, err)

	counts := k.GetReactionCounts(ctx, 0, 0)
	require.Equal(t, uint64(1), counts.LikeCount)

	// React with INSIGHTFUL (different nullifier)
	_, err = ms.AnonymousReact(ctx, &types.MsgAnonymousReact{
		Submitter:     submitter,
		PostId:        0,
		ReactionType:  types.ReactionType_REACTION_TYPE_INSIGHTFUL,
		Proof:         []byte("fake-proof"),
		Nullifier:     []byte("nullifier-insightful-1"),
		MerkleRoot:    []byte("mock-trust-tree-root"),
		MinTrustLevel: 1,
	})
	require.NoError(t, err)

	counts = k.GetReactionCounts(ctx, 0, 0)
	require.Equal(t, uint64(1), counts.LikeCount)
	require.Equal(t, uint64(1), counts.InsightfulCount)
}

func TestAnonymousReactNullifierRecorded(t *testing.T) {
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	k, ms, ctx, _, _, _ := setupAnonPostMsgServer(t)

	k.SetPost(ctx, types.Post{
		Id:     0,
		Status: types.PostStatus_POST_STATUS_ACTIVE,
	})
	k.SetPostCount(ctx, 1)

	nullifier := []byte("react-nullifier-record")
	_, err := ms.AnonymousReact(ctx, &types.MsgAnonymousReact{
		Submitter:     submitter,
		PostId:        0,
		ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
		Proof:         []byte("fake-proof"),
		Nullifier:     nullifier,
		MerkleRoot:    []byte("mock-trust-tree-root"),
		MinTrustLevel: 1,
	})
	require.NoError(t, err)

	// Verify nullifier was recorded (domain 8 = anonymous post reaction)
	require.True(t, k.IsNullifierUsed(ctx, 8, 0, fmt.Sprintf("%x", nullifier)))
}

func TestAnonymousReactFeeChargedAndBurned(t *testing.T) {
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	k, ms, ctx, bk, _, _ := setupAnonPostMsgServer(t)

	// Set reaction fee
	params, _ := k.Params.Get(ctx)
	params.ReactionFee = sdk.NewCoin("uspark", math.NewInt(100))
	params.ReactionFeeExempt = false
	k.Params.Set(ctx, params)

	k.SetPost(ctx, types.Post{
		Id:     0,
		Status: types.PostStatus_POST_STATUS_ACTIVE,
	})
	k.SetPostCount(ctx, 1)

	_, err := ms.AnonymousReact(ctx, &types.MsgAnonymousReact{
		Submitter:     submitter,
		PostId:        0,
		ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
		Proof:         []byte("fake-proof"),
		Nullifier:     []byte("fee-nullifier"),
		MerkleRoot:    []byte("mock-trust-tree-root"),
		MinTrustLevel: 1,
	})
	require.NoError(t, err)

	// Verify fee was sent to module and burned
	require.Len(t, bk.SendCoinsFromAccountToModuleCalls, 1)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))), bk.SendCoinsFromAccountToModuleCalls[0].Amt)
	require.Len(t, bk.BurnCoinsCalls, 1)
}

func TestAnonymousReactFeeExempt(t *testing.T) {
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	k, ms, ctx, bk, _, _ := setupAnonPostMsgServer(t)

	// Set reaction fee exempt
	params, _ := k.Params.Get(ctx)
	params.ReactionFee = sdk.NewCoin("uspark", math.NewInt(100))
	params.ReactionFeeExempt = true
	k.Params.Set(ctx, params)

	k.SetPost(ctx, types.Post{
		Id:     0,
		Status: types.PostStatus_POST_STATUS_ACTIVE,
	})
	k.SetPostCount(ctx, 1)

	_, err := ms.AnonymousReact(ctx, &types.MsgAnonymousReact{
		Submitter:     submitter,
		PostId:        0,
		ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
		Proof:         []byte("fake-proof"),
		Nullifier:     []byte("exempt-nullifier"),
		MerkleRoot:    []byte("mock-trust-tree-root"),
		MinTrustLevel: 1,
	})
	require.NoError(t, err)

	// No fee calls
	require.Len(t, bk.SendCoinsFromAccountToModuleCalls, 0)
	require.Len(t, bk.BurnCoinsCalls, 0)
}

func TestAnonymousReactPreviousMerkleRootAccepted(t *testing.T) {
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	k, ms, ctx, _, rk, _ := setupAnonPostMsgServer(t)

	// Set up previous root to differ from current
	rk.GetMemberTrustTreeRootFn = func(ctx context.Context) ([]byte, error) {
		return []byte("new-root"), nil
	}
	rk.GetPreviousMemberTrustTreeRootFn = func(ctx context.Context) []byte {
		return []byte("old-root")
	}

	k.SetPost(ctx, types.Post{
		Id:     0,
		Status: types.PostStatus_POST_STATUS_ACTIVE,
	})
	k.SetPostCount(ctx, 1)

	// Using old root should succeed
	_, err := ms.AnonymousReact(ctx, &types.MsgAnonymousReact{
		Submitter:     submitter,
		PostId:        0,
		ReactionType:  types.ReactionType_REACTION_TYPE_LIKE,
		Proof:         []byte("fake-proof"),
		Nullifier:     []byte("prev-root-nullifier"),
		MerkleRoot:    []byte("old-root"),
		MinTrustLevel: 1,
	})
	require.NoError(t, err)
}
