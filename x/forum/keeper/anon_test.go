package keeper_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

// --- Nullifier CRUD ---

func TestNullifier_SetAndCheck(t *testing.T) {
	f := initFixture(t)

	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 3, 100, "abc123"))

	f.keeper.SetNullifierUsed(f.ctx, 3, 100, "abc123", types.AnonNullifierEntry{
		UsedAt: 12345,
		Domain: 3,
		Scope:  100,
	})

	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 3, 100, "abc123"))
}

func TestNullifier_DifferentDomain(t *testing.T) {
	f := initFixture(t)

	f.keeper.SetNullifierUsed(f.ctx, 3, 100, "abc123", types.AnonNullifierEntry{
		UsedAt: 1, Domain: 3, Scope: 100,
	})

	// Same nullifier hex, different domain → not found
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 4, 100, "abc123"))
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 5, 100, "abc123"))
	// Original still found
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 3, 100, "abc123"))
}

func TestNullifier_DifferentScope(t *testing.T) {
	f := initFixture(t)

	f.keeper.SetNullifierUsed(f.ctx, 3, 100, "abc123", types.AnonNullifierEntry{
		UsedAt: 1, Domain: 3, Scope: 100,
	})

	// Same nullifier hex, same domain, different scope → not found
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 3, 200, "abc123"))
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 3, 0, "abc123"))
}

func TestNullifier_DifferentHex(t *testing.T) {
	f := initFixture(t)

	f.keeper.SetNullifierUsed(f.ctx, 3, 100, "abc123", types.AnonNullifierEntry{
		UsedAt: 1, Domain: 3, Scope: 100,
	})

	// Different hex string → not found
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 3, 100, "def456"))
}

func TestNullifier_MultipleEntries(t *testing.T) {
	f := initFixture(t)

	// Store multiple nullifiers across different domains and scopes
	f.keeper.SetNullifierUsed(f.ctx, 3, 10, "n1", types.AnonNullifierEntry{UsedAt: 1, Domain: 3, Scope: 10})
	f.keeper.SetNullifierUsed(f.ctx, 4, 20, "n2", types.AnonNullifierEntry{UsedAt: 2, Domain: 4, Scope: 20})
	f.keeper.SetNullifierUsed(f.ctx, 5, 30, "n3", types.AnonNullifierEntry{UsedAt: 3, Domain: 5, Scope: 30})

	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 3, 10, "n1"))
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 4, 20, "n2"))
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 5, 30, "n3"))

	// Cross-references should not match
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 3, 10, "n2"))
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 4, 20, "n1"))
}

// --- Post Metadata CRUD ---

func TestAnonymousPostMeta_SetAndGet(t *testing.T) {
	f := initFixture(t)

	meta := types.AnonymousPostMetadata{
		ContentId:        1,
		Nullifier:        []byte("nullifier-bytes"),
		MerkleRoot:       []byte("merkle-root-bytes"),
		ProvenTrustLevel: 2,
	}
	f.keeper.SetAnonymousPostMeta(f.ctx, 1, meta)

	got, found := f.keeper.GetAnonymousPostMeta(f.ctx, 1)
	require.True(t, found)
	require.Equal(t, uint64(1), got.ContentId)
	require.Equal(t, []byte("nullifier-bytes"), got.Nullifier)
	require.Equal(t, []byte("merkle-root-bytes"), got.MerkleRoot)
	require.Equal(t, uint32(2), got.ProvenTrustLevel)
}

func TestAnonymousPostMeta_NotFound(t *testing.T) {
	f := initFixture(t)

	_, found := f.keeper.GetAnonymousPostMeta(f.ctx, 999)
	require.False(t, found)
}

func TestAnonymousPostMeta_Overwrite(t *testing.T) {
	f := initFixture(t)

	meta1 := types.AnonymousPostMetadata{ContentId: 1, ProvenTrustLevel: 2}
	f.keeper.SetAnonymousPostMeta(f.ctx, 1, meta1)

	meta2 := types.AnonymousPostMetadata{ContentId: 1, ProvenTrustLevel: 4}
	f.keeper.SetAnonymousPostMeta(f.ctx, 1, meta2)

	got, found := f.keeper.GetAnonymousPostMeta(f.ctx, 1)
	require.True(t, found)
	require.Equal(t, uint32(4), got.ProvenTrustLevel)
}

// --- Reply Metadata CRUD ---

func TestAnonymousReplyMeta_SetAndGet(t *testing.T) {
	f := initFixture(t)

	meta := types.AnonymousPostMetadata{
		ContentId:        10,
		Nullifier:        []byte("reply-nullifier"),
		MerkleRoot:       []byte("reply-root"),
		ProvenTrustLevel: 3,
	}
	f.keeper.SetAnonymousReplyMeta(f.ctx, 10, meta)

	got, found := f.keeper.GetAnonymousReplyMeta(f.ctx, 10)
	require.True(t, found)
	require.Equal(t, uint64(10), got.ContentId)
	require.Equal(t, uint32(3), got.ProvenTrustLevel)
}

func TestAnonymousReplyMeta_NotFound(t *testing.T) {
	f := initFixture(t)

	_, found := f.keeper.GetAnonymousReplyMeta(f.ctx, 999)
	require.False(t, found)
}

func TestAnonymousReplyMeta_IsolatedFromPostMeta(t *testing.T) {
	f := initFixture(t)

	// Set post meta for ID 5
	postMeta := types.AnonymousPostMetadata{ContentId: 5, ProvenTrustLevel: 2}
	f.keeper.SetAnonymousPostMeta(f.ctx, 5, postMeta)

	// Reply meta for same ID should not be found (different prefix)
	_, found := f.keeper.GetAnonymousReplyMeta(f.ctx, 5)
	require.False(t, found)

	// Set reply meta for same ID
	replyMeta := types.AnonymousPostMetadata{ContentId: 5, ProvenTrustLevel: 4}
	f.keeper.SetAnonymousReplyMeta(f.ctx, 5, replyMeta)

	// Both should exist independently
	gotPost, foundPost := f.keeper.GetAnonymousPostMeta(f.ctx, 5)
	require.True(t, foundPost)
	require.Equal(t, uint32(2), gotPost.ProvenTrustLevel)

	gotReply, foundReply := f.keeper.GetAnonymousReplyMeta(f.ctx, 5)
	require.True(t, foundReply)
	require.Equal(t, uint32(4), gotReply.ProvenTrustLevel)
}

// --- Export Functions ---

func TestExportAnonymousPostMeta_Empty(t *testing.T) {
	f := initFixture(t)

	result := f.keeper.ExportAnonymousPostMeta(f.ctx)
	require.Empty(t, result)
}

func TestExportAnonymousPostMeta_Multiple(t *testing.T) {
	f := initFixture(t)

	f.keeper.SetAnonymousPostMeta(f.ctx, 1, types.AnonymousPostMetadata{ContentId: 1, ProvenTrustLevel: 2})
	f.keeper.SetAnonymousPostMeta(f.ctx, 2, types.AnonymousPostMetadata{ContentId: 2, ProvenTrustLevel: 3})
	f.keeper.SetAnonymousPostMeta(f.ctx, 3, types.AnonymousPostMetadata{ContentId: 3, ProvenTrustLevel: 4})

	result := f.keeper.ExportAnonymousPostMeta(f.ctx)
	require.Len(t, result, 3)

	// Verify all entries present (iterator order is key-sorted)
	ids := make(map[uint64]bool)
	for _, m := range result {
		ids[m.ContentId] = true
	}
	require.True(t, ids[1])
	require.True(t, ids[2])
	require.True(t, ids[3])
}

func TestExportAnonymousReplyMeta_Empty(t *testing.T) {
	f := initFixture(t)

	result := f.keeper.ExportAnonymousReplyMeta(f.ctx)
	require.Empty(t, result)
}

func TestExportAnonymousReplyMeta_Multiple(t *testing.T) {
	f := initFixture(t)

	f.keeper.SetAnonymousReplyMeta(f.ctx, 10, types.AnonymousPostMetadata{ContentId: 10, ProvenTrustLevel: 2})
	f.keeper.SetAnonymousReplyMeta(f.ctx, 20, types.AnonymousPostMetadata{ContentId: 20, ProvenTrustLevel: 3})

	result := f.keeper.ExportAnonymousReplyMeta(f.ctx)
	require.Len(t, result, 2)
}

func TestExportNullifiers_Empty(t *testing.T) {
	f := initFixture(t)

	result := f.keeper.ExportNullifiers(f.ctx)
	require.Empty(t, result)
}

func TestExportNullifiers_Multiple(t *testing.T) {
	f := initFixture(t)

	f.keeper.SetNullifierUsed(f.ctx, 3, 100, "aaa", types.AnonNullifierEntry{UsedAt: 1, Domain: 3, Scope: 100})
	f.keeper.SetNullifierUsed(f.ctx, 4, 200, "bbb", types.AnonNullifierEntry{UsedAt: 2, Domain: 4, Scope: 200})
	f.keeper.SetNullifierUsed(f.ctx, 5, 300, "ccc", types.AnonNullifierEntry{UsedAt: 3, Domain: 5, Scope: 300})

	result := f.keeper.ExportNullifiers(f.ctx)
	require.Len(t, result, 3)

	// Verify domain/scope/hex are correctly decoded from the key
	for _, entry := range result {
		require.NotNil(t, entry.Entry)
		require.Equal(t, entry.Domain, entry.Entry.Domain)
		require.Equal(t, entry.Scope, entry.Entry.Scope)
	}
}

func TestExportNullifiers_KeyDecoding(t *testing.T) {
	f := initFixture(t)

	// Use specific values so we can verify decoding
	f.keeper.SetNullifierUsed(f.ctx, 3, 42, "deadbeef", types.AnonNullifierEntry{UsedAt: 99, Domain: 3, Scope: 42})

	result := f.keeper.ExportNullifiers(f.ctx)
	require.Len(t, result, 1)

	entry := result[0]
	require.Equal(t, uint64(3), entry.Domain)
	require.Equal(t, uint64(42), entry.Scope)
	require.Equal(t, "deadbeef", entry.NullifierHex)
	require.Equal(t, int64(99), entry.Entry.UsedAt)
}

// --- GetPostIDBytes ---

func TestGetPostIDBytes(t *testing.T) {
	// Test zero
	b := keeper.GetPostIDBytes(0)
	require.Len(t, b, 8)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 0}, b)

	// Test 1
	b = keeper.GetPostIDBytes(1)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 1}, b)

	// Test max uint64
	b = keeper.GetPostIDBytes(^uint64(0))
	require.Equal(t, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, b)

	// Test a mid-range value (256)
	b = keeper.GetPostIDBytes(256)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 1, 0}, b)
}
