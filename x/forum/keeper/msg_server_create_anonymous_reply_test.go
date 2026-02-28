package keeper_test

import (
	"bytes"
	"testing"

	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestCreateAnonymousReply_Success(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	params.AnonymousMinTrustLevel = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Create category with anonymous allowed
	cat := f.createTestCategory(t, "anon-cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	// Create root post to reply to
	rootPost := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	merkleRoot := bytes.Repeat([]byte{0xAA}, 32)

	resp, err := f.msgServer.CreateAnonymousReply(f.ctx, &types.MsgCreateAnonymousReply{
		Submitter:     testCreator2,
		ParentId:      rootPost.PostId,
		Content:       "Anonymous reply",
		Proof:         []byte("fake-proof"),
		Nullifier:     bytes.Repeat([]byte{0x02}, 32),
		MerkleRoot:    merkleRoot,
		MinTrustLevel: 2,
	})
	require.NoError(t, err)
	require.NotZero(t, resp.Id)

	// Verify reply was created
	reply, err := f.keeper.Post.Get(f.ctx, resp.Id)
	require.NoError(t, err)
	require.Equal(t, "Anonymous reply", reply.Content)
	require.Equal(t, rootPost.PostId, reply.ParentId)
	require.Equal(t, rootPost.PostId, reply.RootId)
	require.Equal(t, uint64(1), reply.Depth)

	// Verify anonymous metadata
	meta, found := f.keeper.GetAnonymousReplyMeta(f.ctx, resp.Id)
	require.True(t, found)
	require.Equal(t, resp.Id, meta.ContentId)

	// Verify nullifier recorded (domain=4, scope=rootID)
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 4, rootPost.PostId, "0202020202020202020202020202020202020202020202020202020202020202"))
}

func TestCreateAnonymousReply_ThreadLocked(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	// Create and lock root post
	rootPost := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	rootPost.Locked = true
	require.NoError(t, f.keeper.Post.Set(f.ctx, rootPost.PostId, rootPost))

	_, err := f.msgServer.CreateAnonymousReply(f.ctx, &types.MsgCreateAnonymousReply{
		Submitter:     testCreator2,
		ParentId:      rootPost.PostId,
		Content:       "reply to locked thread",
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x02}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "locked")
}

func TestCreateAnonymousReply_ParentNotFound(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	_, err := f.msgServer.CreateAnonymousReply(f.ctx, &types.MsgCreateAnonymousReply{
		Submitter:     testCreator,
		ParentId:      9999,
		Content:       "orphan reply",
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x02}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestCreateAnonymousReply_MaxDepthExceeded(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	params.MaxReplyDepth = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	// Create root post
	rootPost := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Create depth-1 reply
	reply1 := f.createTestPost(t, testCreator, rootPost.PostId, cat.CategoryId)
	reply1.Depth = 1
	reply1.RootId = rootPost.PostId
	require.NoError(t, f.keeper.Post.Set(f.ctx, reply1.PostId, reply1))

	// Try to reply to depth-1 (would create depth-2, exceeding max of 1)
	_, err := f.msgServer.CreateAnonymousReply(f.ctx, &types.MsgCreateAnonymousReply{
		Submitter:     testCreator2,
		ParentId:      reply1.PostId,
		Content:       "too deep",
		Proof:         []byte("proof"),
		Nullifier:     bytes.Repeat([]byte{0x02}, 32),
		MerkleRoot:    bytes.Repeat([]byte{0xAA}, 32),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "max depth")
}

func TestCreateAnonymousReply_DuplicateNullifier(t *testing.T) {
	f, _ := initFixtureWithVoteKeeper(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cat := f.createTestCategory(t, "cat")
	cat.AllowAnonymous = true
	require.NoError(t, f.keeper.Category.Set(f.ctx, cat.CategoryId, cat))

	rootPost := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	merkleRoot := bytes.Repeat([]byte{0xAA}, 32)
	nullifier := bytes.Repeat([]byte{0x02}, 32)

	// First reply succeeds
	_, err := f.msgServer.CreateAnonymousReply(f.ctx, &types.MsgCreateAnonymousReply{
		Submitter:     testCreator2,
		ParentId:      rootPost.PostId,
		Content:       "first reply",
		Proof:         []byte("proof"),
		Nullifier:     nullifier,
		MerkleRoot:    merkleRoot,
		MinTrustLevel: 2,
	})
	require.NoError(t, err)

	// Second reply with same nullifier fails
	_, err = f.msgServer.CreateAnonymousReply(f.ctx, &types.MsgCreateAnonymousReply{
		Submitter:     testCreator2,
		ParentId:      rootPost.PostId,
		Content:       "second reply",
		Proof:         []byte("proof"),
		Nullifier:     nullifier,
		MerkleRoot:    merkleRoot,
		MinTrustLevel: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nullifier already used")
}
