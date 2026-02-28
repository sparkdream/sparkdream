package keeper_test

import (
	"bytes"
	"context"
	"fmt"
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

type mockVoteKeeperPost struct {
	VerifyAnonymousActionProofFn func(ctx context.Context, proof, nullifier, merkleRoot []byte, minTrustLevel uint32) error
}

func (m *mockVoteKeeperPost) VerifyAnonymousActionProof(ctx context.Context, proof, nullifier, merkleRoot []byte, minTrustLevel uint32) error {
	if m.VerifyAnonymousActionProofFn != nil {
		return m.VerifyAnonymousActionProofFn(ctx, proof, nullifier, merkleRoot, minTrustLevel)
	}
	return nil // dev mode: accept all
}

func setupAnonPostMsgServer(t testing.TB) (keeper.Keeper, types.MsgServer, sdk.Context, *mockBankKeeper, *mockRepKeeper, *mockVoteKeeperPost) {
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec("sprkdrm")
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	authority := authtypes.NewModuleAddress(types.GovModuleName)
	bankKeeper := &mockBankKeeper{}
	repKeeper := &mockRepKeeper{}
	voteKeeper := &mockVoteKeeperPost{}
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

func TestCreateAnonymousPost(t *testing.T) {
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	tests := []struct {
		name        string
		setup       func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer)
		msg         *types.MsgCreateAnonymousPost
		useNoVote   bool // if true, use setupMsgServer (no VoteKeeper)
		expectError bool
		errContains string
	}{
		{
			name: "successful anonymous post creation",
			msg: &types.MsgCreateAnonymousPost{
				Submitter:     submitter,
				Title:         "Anonymous Post",
				Body:          "This is an anonymous post body",
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
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
			msg: &types.MsgCreateAnonymousPost{
				Submitter:     submitter,
				Title:         "Disabled Anon Post",
				Body:          "Should fail",
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
			msg: &types.MsgCreateAnonymousPost{
				Submitter:     submitter,
				Title:         "No VoteKeeper",
				Body:          "Should fail without vote module",
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "vote module not available",
		},
		{
			name: "empty title",
			msg: &types.MsgCreateAnonymousPost{
				Submitter:     submitter,
				Title:         "",
				Body:          "Body with empty title",
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier-empty-title"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "title cannot be empty",
		},
		{
			name: "title too long",
			msg: &types.MsgCreateAnonymousPost{
				Submitter:     submitter,
				Title:         string(bytes.Repeat([]byte("a"), 201)),
				Body:          "Valid body",
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier-long-title"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "title too long",
		},
		{
			name: "body too long",
			msg: &types.MsgCreateAnonymousPost{
				Submitter:     submitter,
				Title:         "Valid Title",
				Body:          string(bytes.Repeat([]byte("a"), 10001)),
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier-long-body"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "body too long",
		},
		{
			name: "min trust level below params minimum",
			msg: &types.MsgCreateAnonymousPost{
				Submitter:     submitter,
				Title:         "Low Trust",
				Body:          "Should fail with low trust level",
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier-low-trust"),
				MerkleRoot:    []byte("mock-trust-tree-root"),
				MinTrustLevel: 0, // params requires 1
			},
			expectError: true,
			errContains: "min_trust_level 0 below required 1",
		},
		{
			name: "invalid/stale merkle root",
			msg: &types.MsgCreateAnonymousPost{
				Submitter:     submitter,
				Title:         "Stale Root",
				Body:          "Should fail with wrong root",
				Proof:         []byte("fake-proof"),
				Nullifier:     []byte("fake-nullifier-stale-root"),
				MerkleRoot:    []byte("wrong-merkle-root"),
				MinTrustLevel: 1,
			},
			expectError: true,
			errContains: "stale or invalid merkle root",
		},
		{
			name: "nullifier already used",
			setup: func(k *keeper.Keeper, ctx sdk.Context, vk *mockVoteKeeperPost, rk *mockRepKeeper, ms types.MsgServer) {
				// Submit a post first with the same nullifier
				_, err := ms.CreateAnonymousPost(ctx, &types.MsgCreateAnonymousPost{
					Submitter:     submitter,
					Title:         "First Post",
					Body:          "First anonymous post",
					Proof:         []byte("fake-proof"),
					Nullifier:     []byte("duplicate-nullifier"),
					MerkleRoot:    []byte("mock-trust-tree-root"),
					MinTrustLevel: 1,
				})
				require.NoError(t, err)
			},
			msg: &types.MsgCreateAnonymousPost{
				Submitter:     submitter,
				Title:         "Duplicate Nullifier",
				Body:          "Should fail with used nullifier",
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
				vk.VerifyAnonymousActionProofFn = func(ctx context.Context, proof, nullifier, merkleRoot []byte, minTrustLevel uint32) error {
					return fmt.Errorf("proof verification failed")
				}
			},
			msg: &types.MsgCreateAnonymousPost{
				Submitter:     submitter,
				Title:         "Bad Proof",
				Body:          "Should fail with bad proof",
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
				// Use setupMsgServer which does NOT wire a VoteKeeper
				var bk *mockBankKeeper
				k, ms, ctx, bk = setupMsgServer(t)
				_ = bk
				// Need to enable anonymous posting in params for this path to reach the voteKeeper check
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

			resp, err := ms.CreateAnonymousPost(ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify post was created
				post, found := k.GetPost(ctx, resp.Id)
				require.True(t, found)
				require.Equal(t, tt.msg.Title, post.Title)
				require.Equal(t, tt.msg.Body, post.Body)
				require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, post.Status)
				require.True(t, post.RepliesEnabled)
			}
		})
	}
}

func TestCreateAnonymousPostCreatorIsModuleAddress(t *testing.T) {
	k, ms, ctx, _, _, _ := setupAnonPostMsgServer(t)
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()

	resp, err := ms.CreateAnonymousPost(ctx, &types.MsgCreateAnonymousPost{
		Submitter:     submitter,
		Title:         "Verify Creator",
		Body:          "Creator should be module address",
		Proof:         []byte("fake-proof"),
		Nullifier:     []byte("nullifier-creator-check"),
		MerkleRoot:    []byte("mock-trust-tree-root"),
		MinTrustLevel: 1,
	})
	require.NoError(t, err)

	post, found := k.GetPost(ctx, resp.Id)
	require.True(t, found)
	require.Equal(t, moduleAddr, post.Creator, "anonymous post creator should be the blog module address")
	require.NotEqual(t, submitter, post.Creator, "anonymous post creator must not be the submitter")
}

func TestCreateAnonymousPostMetadataStored(t *testing.T) {
	k, ms, ctx, _, _, _ := setupAnonPostMsgServer(t)
	submitter := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	nullifier := []byte("nullifier-meta-check")
	merkleRoot := []byte("mock-trust-tree-root")

	resp, err := ms.CreateAnonymousPost(ctx, &types.MsgCreateAnonymousPost{
		Submitter:     submitter,
		Title:         "Verify Metadata",
		Body:          "Metadata should be stored",
		Proof:         []byte("fake-proof"),
		Nullifier:     nullifier,
		MerkleRoot:    merkleRoot,
		MinTrustLevel: 1,
	})
	require.NoError(t, err)

	meta, found := k.GetAnonymousPostMeta(ctx, resp.Id)
	require.True(t, found, "anonymous post metadata should be stored")
	require.Equal(t, resp.Id, meta.ContentId)
	require.Equal(t, nullifier, meta.Nullifier)
	require.Equal(t, merkleRoot, meta.MerkleRoot)
	require.Equal(t, uint32(1), meta.ProvenTrustLevel)
}
