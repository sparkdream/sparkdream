package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

func TestIsNullifierUsed(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	tests := []struct {
		name         string
		domain       uint64
		scope        uint64
		nullifierHex string
	}{
		{
			name:         "domain 1 scope 1",
			domain:       1,
			scope:        1,
			nullifierHex: "abc123",
		},
		{
			name:         "domain 1 scope 2",
			domain:       1,
			scope:        2,
			nullifierHex: "abc123",
		},
		{
			name:         "domain 2 scope 1",
			domain:       2,
			scope:        1,
			nullifierHex: "abc123",
		},
		{
			name:         "different nullifier same domain scope",
			domain:       1,
			scope:        1,
			nullifierHex: "def456",
		},
	}

	// All should be unused initially
	for _, tt := range tests {
		t.Run(tt.name+" initially unused", func(t *testing.T) {
			used := k.IsNullifierUsed(ctx, tt.domain, tt.scope, tt.nullifierHex)
			require.False(t, used)
		})
	}

	// Mark the first one as used
	entry := types.AnonNullifierEntry{
		UsedAt: 100,
		Domain: 1,
		Scope:  1,
	}
	k.SetNullifierUsed(ctx, 1, 1, "abc123", entry)

	t.Run("marked nullifier is used", func(t *testing.T) {
		used := k.IsNullifierUsed(ctx, 1, 1, "abc123")
		require.True(t, used)
	})

	t.Run("same nullifier different scope is still unused", func(t *testing.T) {
		used := k.IsNullifierUsed(ctx, 1, 2, "abc123")
		require.False(t, used)
	})

	t.Run("same nullifier different domain is still unused", func(t *testing.T) {
		used := k.IsNullifierUsed(ctx, 2, 1, "abc123")
		require.False(t, used)
	})

	t.Run("different nullifier same domain scope is still unused", func(t *testing.T) {
		used := k.IsNullifierUsed(ctx, 1, 1, "def456")
		require.False(t, used)
	})

	// Mark a second one as used and verify independence
	entry2 := types.AnonNullifierEntry{
		UsedAt: 200,
		Domain: 2,
		Scope:  1,
	}
	k.SetNullifierUsed(ctx, 2, 1, "abc123", entry2)

	t.Run("second marked nullifier is used", func(t *testing.T) {
		used := k.IsNullifierUsed(ctx, 2, 1, "abc123")
		require.True(t, used)
	})

	t.Run("first marked nullifier still used", func(t *testing.T) {
		used := k.IsNullifierUsed(ctx, 1, 1, "abc123")
		require.True(t, used)
	})
}

func TestAnonymousPostMeta(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	tests := []struct {
		name   string
		postId uint64
		meta   types.AnonymousPostMetadata
	}{
		{
			name:   "basic post metadata",
			postId: 1,
			meta: types.AnonymousPostMetadata{
				ContentId:        1,
				Nullifier:        []byte("nullifier-one"),
				MerkleRoot:       []byte("root-one"),
				ProvenTrustLevel: 3,
			},
		},
		{
			name:   "different post metadata",
			postId: 42,
			meta: types.AnonymousPostMetadata{
				ContentId:        42,
				Nullifier:        []byte("nullifier-two"),
				MerkleRoot:       []byte("root-two"),
				ProvenTrustLevel: 5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Before setting, should not be found
			_, found := k.GetAnonymousPostMeta(ctx, tt.postId)
			require.False(t, found, "should not find metadata before setting")

			// Set and retrieve
			k.SetAnonymousPostMeta(ctx, tt.postId, tt.meta)
			got, found := k.GetAnonymousPostMeta(ctx, tt.postId)
			require.True(t, found, "should find metadata after setting")
			require.Equal(t, tt.meta.ContentId, got.ContentId)
			require.Equal(t, tt.meta.Nullifier, got.Nullifier)
			require.Equal(t, tt.meta.MerkleRoot, got.MerkleRoot)
			require.Equal(t, tt.meta.ProvenTrustLevel, got.ProvenTrustLevel)
		})
	}

	t.Run("non-existent postId returns not found", func(t *testing.T) {
		_, found := k.GetAnonymousPostMeta(ctx, 99999)
		require.False(t, found)
	})
}

func TestAnonymousReplyMeta(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	tests := []struct {
		name    string
		replyId uint64
		meta    types.AnonymousPostMetadata
	}{
		{
			name:    "basic reply metadata",
			replyId: 10,
			meta: types.AnonymousPostMetadata{
				ContentId:        10,
				Nullifier:        []byte("reply-nullifier-one"),
				MerkleRoot:       []byte("reply-root-one"),
				ProvenTrustLevel: 2,
			},
		},
		{
			name:    "different reply metadata",
			replyId: 77,
			meta: types.AnonymousPostMetadata{
				ContentId:        77,
				Nullifier:        []byte("reply-nullifier-two"),
				MerkleRoot:       []byte("reply-root-two"),
				ProvenTrustLevel: 4,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Before setting, should not be found
			_, found := k.GetAnonymousReplyMeta(ctx, tt.replyId)
			require.False(t, found, "should not find metadata before setting")

			// Set and retrieve
			k.SetAnonymousReplyMeta(ctx, tt.replyId, tt.meta)
			got, found := k.GetAnonymousReplyMeta(ctx, tt.replyId)
			require.True(t, found, "should find metadata after setting")
			require.Equal(t, tt.meta.ContentId, got.ContentId)
			require.Equal(t, tt.meta.Nullifier, got.Nullifier)
			require.Equal(t, tt.meta.MerkleRoot, got.MerkleRoot)
			require.Equal(t, tt.meta.ProvenTrustLevel, got.ProvenTrustLevel)
		})
	}

	t.Run("non-existent replyId returns not found", func(t *testing.T) {
		_, found := k.GetAnonymousReplyMeta(ctx, 99999)
		require.False(t, found)
	})

	t.Run("reply meta does not collide with post meta", func(t *testing.T) {
		postMeta := types.AnonymousPostMetadata{
			ContentId:        5,
			Nullifier:        []byte("post-null"),
			MerkleRoot:       []byte("post-root"),
			ProvenTrustLevel: 1,
		}
		replyMeta := types.AnonymousPostMetadata{
			ContentId:        5,
			Nullifier:        []byte("reply-null"),
			MerkleRoot:       []byte("reply-root"),
			ProvenTrustLevel: 7,
		}

		k.SetAnonymousPostMeta(ctx, 5, postMeta)
		k.SetAnonymousReplyMeta(ctx, 5, replyMeta)

		gotPost, found := k.GetAnonymousPostMeta(ctx, 5)
		require.True(t, found)
		require.Equal(t, []byte("post-null"), gotPost.Nullifier)

		gotReply, found := k.GetAnonymousReplyMeta(ctx, 5)
		require.True(t, found)
		require.Equal(t, []byte("reply-null"), gotReply.Nullifier)
	})
}

func TestAnonSubsidyLastEpoch(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	t.Run("initially returns 0", func(t *testing.T) {
		epoch := k.GetAnonSubsidyLastEpoch(ctx)
		require.Equal(t, uint64(0), epoch)
	})

	tests := []struct {
		name  string
		epoch uint64
	}{
		{
			name:  "set epoch 1",
			epoch: 1,
		},
		{
			name:  "set epoch 100",
			epoch: 100,
		},
		{
			name:  "set large epoch",
			epoch: 999999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k.SetAnonSubsidyLastEpoch(ctx, tt.epoch)
			got := k.GetAnonSubsidyLastEpoch(ctx)
			require.Equal(t, tt.epoch, got)
		})
	}

	t.Run("overwrite preserves latest value", func(t *testing.T) {
		k.SetAnonSubsidyLastEpoch(ctx, 50)
		k.SetAnonSubsidyLastEpoch(ctx, 75)
		got := k.GetAnonSubsidyLastEpoch(ctx)
		require.Equal(t, uint64(75), got)
	})
}
