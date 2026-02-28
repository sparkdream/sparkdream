package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestNullifierGetSet(t *testing.T) {
	f := initTestFixture(t)

	domain := uint64(6)
	scope := uint64(100)
	nullifierHex := "abcdef1234567890"

	// Initially not used
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, domain, scope, nullifierHex))

	// Set nullifier as used
	entry := types.AnonNullifierEntry{
		UsedAt: 50,
		Domain: domain,
		Scope:  scope,
	}
	f.keeper.SetNullifierUsed(f.ctx, domain, scope, nullifierHex, entry)

	// Now it should be used
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, domain, scope, nullifierHex))

	// Different nullifier in same domain/scope should not be used
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, domain, scope, "different_nullifier"))

	// Same nullifier in different domain should not be used
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, domain+1, scope, nullifierHex))

	// Same nullifier in different scope should not be used
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, domain, scope+1, nullifierHex))
}

func TestAnonymousCollectionMetaGetSet(t *testing.T) {
	f := initTestFixture(t)

	collID := uint64(42)
	mgmtKey := make([]byte, 32)
	for i := range mgmtKey {
		mgmtKey[i] = byte(i)
	}

	// Initially not found
	_, found := f.keeper.GetAnonymousCollectionMeta(f.ctx, collID)
	require.False(t, found)

	// Set metadata
	meta := types.AnonymousCollectionMeta{
		CollectionId:        collID,
		ManagementPublicKey: mgmtKey,
		Nullifier:           []byte("test_nullifier"),
		MerkleRoot:          []byte("test_root"),
		Nonce:               0,
		ProvenTrustLevel:    2,
	}
	f.keeper.SetAnonymousCollectionMeta(f.ctx, collID, meta)

	// Retrieve and verify
	got, found := f.keeper.GetAnonymousCollectionMeta(f.ctx, collID)
	require.True(t, found)
	require.Equal(t, collID, got.CollectionId)
	require.Equal(t, mgmtKey, got.ManagementPublicKey)
	require.Equal(t, uint64(0), got.Nonce)
	require.Equal(t, uint32(2), got.ProvenTrustLevel)

	// Update nonce
	meta.Nonce = 5
	f.keeper.SetAnonymousCollectionMeta(f.ctx, collID, meta)
	got, found = f.keeper.GetAnonymousCollectionMeta(f.ctx, collID)
	require.True(t, found)
	require.Equal(t, uint64(5), got.Nonce)

	// Different collection ID should not be found
	_, found = f.keeper.GetAnonymousCollectionMeta(f.ctx, collID+1)
	require.False(t, found)
}

func TestManagementKeyCollectionCount(t *testing.T) {
	f := initTestFixture(t)

	mgmtKey := make([]byte, 32)
	for i := range mgmtKey {
		mgmtKey[i] = byte(i + 1)
	}
	mgmtKey2 := make([]byte, 32)
	for i := range mgmtKey2 {
		mgmtKey2[i] = byte(i + 100)
	}

	// Initially zero
	require.Equal(t, uint32(0), f.keeper.GetManagementKeyCollectionCount(f.ctx, mgmtKey))
	require.Equal(t, uint32(0), f.keeper.GetManagementKeyCollectionCount(f.ctx, mgmtKey2))

	// Increment key1 once
	f.keeper.IncrementManagementKeyCollectionCount(f.ctx, mgmtKey)
	require.Equal(t, uint32(1), f.keeper.GetManagementKeyCollectionCount(f.ctx, mgmtKey))
	require.Equal(t, uint32(0), f.keeper.GetManagementKeyCollectionCount(f.ctx, mgmtKey2))

	// Increment key1 again
	f.keeper.IncrementManagementKeyCollectionCount(f.ctx, mgmtKey)
	require.Equal(t, uint32(2), f.keeper.GetManagementKeyCollectionCount(f.ctx, mgmtKey))

	// Increment key2
	f.keeper.IncrementManagementKeyCollectionCount(f.ctx, mgmtKey2)
	require.Equal(t, uint32(2), f.keeper.GetManagementKeyCollectionCount(f.ctx, mgmtKey))
	require.Equal(t, uint32(1), f.keeper.GetManagementKeyCollectionCount(f.ctx, mgmtKey2))
}
