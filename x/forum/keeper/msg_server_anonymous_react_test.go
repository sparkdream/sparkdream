package keeper_test

import (
	"bytes"
	"context"
	"testing"

	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestAnonymousReact_Upvote(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.ReactionsEnabled = true
	params.PrivateReactionsEnabled = true
	params.AnonymousMinTrustLevel = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	merkleRoot := bytes.Repeat([]byte{0xAA}, 32)

	resp, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     testCreator2,
		PostId:        post.PostId,
		ReactionType:  1, // upvote
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x03}, 32),
		MerkleRoot:    merkleRoot,
		MinTrustLevel: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify upvote count incremented
	updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
	require.NoError(t, err)
	require.Equal(t, uint64(1), updatedPost.UpvoteCount)
	require.Equal(t, uint64(0), updatedPost.DownvoteCount)
}

func TestAnonymousReact_Downvote(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.ReactionsEnabled = true
	params.PrivateReactionsEnabled = true
	params.AnonymousMinTrustLevel = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	resp, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     testCreator2,
		PostId:        post.PostId,
		ReactionType:  2, // downvote
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x03}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify downvote count incremented
	updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
	require.NoError(t, err)
	require.Equal(t, uint64(0), updatedPost.UpvoteCount)
	require.Equal(t, uint64(1), updatedPost.DownvoteCount)
}

func TestAnonymousReact_ReactionsDisabled(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.ReactionsEnabled = false
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     testCreator2,
		PostId:        post.PostId,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x03}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
}

func TestAnonymousReact_PrivateReactionsDisabled(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.ReactionsEnabled = true
	params.PrivateReactionsEnabled = false
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     testCreator2,
		PostId:        post.PostId,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x03}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "private reactions")
}

func TestAnonymousReact_InvalidReactionType(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.ReactionsEnabled = true
	params.PrivateReactionsEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     testCreator2,
		PostId:        post.PostId,
		ReactionType:  3, // Invalid
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x03}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "reaction_type must be 1")
}

func TestAnonymousReact_PostNotFound(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.ReactionsEnabled = true
	params.PrivateReactionsEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     testCreator2,
		PostId:        9999,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x03}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestAnonymousReact_DuplicateNullifier(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.ReactionsEnabled = true
	params.PrivateReactionsEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	merkleRoot := bytes.Repeat([]byte{0xAA}, 32)
	nullifier := bytes.Repeat([]byte{0x03}, 32)

	// First reaction succeeds
	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     testCreator2,
		PostId:        post.PostId,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     nullifier,
		MerkleRoot:    merkleRoot,
		MinTrustLevel: 2,
	})
	require.NoError(t, err)

	// Second reaction with same nullifier fails
	_, err = f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     testCreator2,
		PostId:        post.PostId,
		ReactionType:  2,
		Proof:         []byte("proof"),
		Nullifier:     nullifier,
		MerkleRoot:    merkleRoot,
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nullifier already used")
}

func TestAnonymousReact_ProofVerificationFails(t *testing.T) {
	f, vk := initFixtureWithVoteKeeper(t)

	// Make proof verification fail
	vk.VerifyAnonymousActionProofFn = func(ctx context.Context, proof, nullifier, merkleRoot []byte, minTrustLevel uint32) error {
		return types.ErrInvalidProof
	}

	params, _ := f.keeper.Params.Get(f.ctx)
	params.ReactionsEnabled = true
	params.PrivateReactionsEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     testCreator2,
		PostId:        post.PostId,
		ReactionType:  1,
		Proof:         []byte("bad-proof"),
		Nullifier:     bytes.Repeat([]byte{0x03}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid ZK proof")
}
