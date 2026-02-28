package keeper_test

import (
	"bytes"
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestCreateAnonymousPost_Success(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	// Enable anonymous posting
	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	params.AnonymousMinTrustLevel = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Create category with anonymous allowed
	cat := f.createTestCategory(t, "anon-cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	merkleRoot := bytes.Repeat([]byte{0xAA}, 32)

	resp, err := f.msgServer.CreateAnonymousPost(f.ctx, &types.MsgCreateAnonymousPost{
		Submitter:     testCreator,
		CategoryId:    cat.CategoryId,
		Content:       "Anonymous test post",
		Proof:         []byte("fake-proof"),
		Nullifier:     bytes.Repeat([]byte{0x01}, 32),
		MerkleRoot:    merkleRoot,
		MinTrustLevel: 2,
	})
	require.NoError(t, err)
	require.NotZero(t, resp.Id)

	// Verify post was created
	post, err := f.keeper.Post.Get(f.ctx, resp.Id)
	require.NoError(t, err)
	require.Equal(t, "Anonymous test post", post.Content)
	require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, post.Status)
	require.Equal(t, cat.CategoryId, post.CategoryId)

	// Verify anonymous metadata stored
	meta, found := f.keeper.GetAnonymousPostMeta(f.ctx, resp.Id)
	require.True(t, found)
	require.Equal(t, resp.Id, meta.ContentId)
	require.Equal(t, uint32(2), meta.ProvenTrustLevel)

	// Verify nullifier recorded (scope is epoch = block_time / epoch_duration)
	sdkCtx := f.sdkCtx()
	epoch := uint64(sdkCtx.BlockTime().Unix()) / uint64(keeper.DefaultEpochDuration)
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 3, epoch, "0101010101010101010101010101010101010101010101010101010101010101"))
}

func TestCreateAnonymousPost_DisabledParam(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = false
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	_, err := f.msgServer.CreateAnonymousPost(f.ctx, &types.MsgCreateAnonymousPost{
		Submitter:     testCreator,
		CategoryId:    cat.CategoryId,
		Content:       "test",
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x01}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "anonymous posting is not enabled")
}

func TestCreateAnonymousPost_NoVoteKeeper(t *testing.T) {
	f := initFixture(t) // No vote keeper set

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	_, err := f.msgServer.CreateAnonymousPost(f.ctx, &types.MsgCreateAnonymousPost{
		Submitter:     testCreator,
		CategoryId:    cat.CategoryId,
		Content:       "test",
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x01}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "vote module not available")
}

func TestCreateAnonymousPost_CategoryNotAllowAnonymous(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "no-anon-cat")
	// AllowAnonymous defaults to false

	_, err := f.msgServer.CreateAnonymousPost(f.ctx, &types.MsgCreateAnonymousPost{
		Submitter:     testCreator,
		CategoryId:    cat.CategoryId,
		Content:       "test",
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x01}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "category does not allow anonymous posts")
}

func TestCreateAnonymousPost_InsufficientTrustLevel(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	params.AnonymousMinTrustLevel = 3
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	_, err := f.msgServer.CreateAnonymousPost(f.ctx, &types.MsgCreateAnonymousPost{
		Submitter:     testCreator,
		CategoryId:    cat.CategoryId,
		Content:       "test",
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x01}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2, // Below required 3
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "min_trust_level")
}

func TestCreateAnonymousPost_DuplicateNullifier(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	merkleRoot := bytes.Repeat([]byte{0xAA}, 32)
	nullifier := bytes.Repeat([]byte{0x01}, 32)

	// First post succeeds
	_, err := f.msgServer.CreateAnonymousPost(f.ctx, &types.MsgCreateAnonymousPost{
		Submitter:     testCreator,
		CategoryId:    cat.CategoryId,
		Content:       "first post",
		Proof:         []byte("proof"),
		Nullifier:     nullifier,
		MerkleRoot:    merkleRoot,
		MinTrustLevel: 2,
	})
	require.NoError(t, err)

	// Second post with same nullifier fails
	_, err = f.msgServer.CreateAnonymousPost(f.ctx, &types.MsgCreateAnonymousPost{
		Submitter:     testCreator,
		CategoryId:    cat.CategoryId,
		Content:       "second post",
		Proof:         []byte("proof"),
		Nullifier:     nullifier,
		MerkleRoot:    merkleRoot,
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nullifier already used")
}

func TestCreateAnonymousPost_EmptyContent(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	_, err := f.msgServer.CreateAnonymousPost(f.ctx, &types.MsgCreateAnonymousPost{
		Submitter:     testCreator,
		CategoryId:    cat.CategoryId,
		Content:       "",
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x01}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
}

func TestCreateAnonymousPost_ForumPaused(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	params.ForumPaused = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	_, err := f.msgServer.CreateAnonymousPost(f.ctx, &types.MsgCreateAnonymousPost{
		Submitter:     testCreator,
		CategoryId:    cat.CategoryId,
		Content:       "test",
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x01}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
}

func TestCreateAnonymousPost_InvalidMerkleRoot(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	// Use a merkle root that doesn't match current or previous
	badRoot := bytes.Repeat([]byte{0xCC}, 32)

	_, err := f.msgServer.CreateAnonymousPost(f.ctx, &types.MsgCreateAnonymousPost{
		Submitter:     testCreator,
		CategoryId:    cat.CategoryId,
		Content:       "test",
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x01}, 32),
		MerkleRoot:    badRoot,
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "stale or invalid merkle root")
}
