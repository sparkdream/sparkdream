package keeper

import (
	"context"
	"encoding/binary"
	"encoding/hex"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"

	"sparkdream/x/collect/types"
)

// IsNullifierUsed checks if a nullifier has been used in the given domain and scope.
func (k Keeper) IsNullifierUsed(ctx context.Context, domain uint64, scope uint64, nullifierHex string) bool {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonNullifierKey))
	key := nullifierKey(domain, scope, nullifierHex)
	return store.Has(key)
}

// SetNullifierUsed marks a nullifier as used.
func (k Keeper) SetNullifierUsed(ctx context.Context, domain uint64, scope uint64, nullifierHex string, entry types.AnonNullifierEntry) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonNullifierKey))
	key := nullifierKey(domain, scope, nullifierHex)
	b := k.cdc.MustMarshal(&entry)
	store.Set(key, b)
}

// SetAnonymousCollectionMeta stores anonymous collection metadata.
func (k Keeper) SetAnonymousCollectionMeta(ctx context.Context, collID uint64, meta types.AnonymousCollectionMeta) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonCollectionMetaKey))
	b := k.cdc.MustMarshal(&meta)
	store.Set(uint64Bytes(collID), b)
}

// GetAnonymousCollectionMeta retrieves anonymous collection metadata.
func (k Keeper) GetAnonymousCollectionMeta(ctx context.Context, collID uint64) (types.AnonymousCollectionMeta, bool) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonCollectionMetaKey))
	b := store.Get(uint64Bytes(collID))
	if b == nil {
		return types.AnonymousCollectionMeta{}, false
	}
	var meta types.AnonymousCollectionMeta
	k.cdc.MustUnmarshal(b, &meta)
	return meta, true
}

// GetManagementKeyCollectionCount returns the number of anonymous collections created
// with the given management public key.
func (k Keeper) GetManagementKeyCollectionCount(ctx context.Context, mgmtKey []byte) uint32 {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonMgmtKeyIndexKey))
	keyHex := hex.EncodeToString(mgmtKey)
	b := store.Get([]byte(keyHex))
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

// IncrementManagementKeyCollectionCount increments the count of anonymous collections
// for the given management public key.
func (k Keeper) IncrementManagementKeyCollectionCount(ctx context.Context, mgmtKey []byte) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonMgmtKeyIndexKey))
	keyHex := hex.EncodeToString(mgmtKey)
	count := k.GetManagementKeyCollectionCount(ctx, mgmtKey) + 1
	bz := make([]byte, 4)
	binary.BigEndian.PutUint32(bz, count)
	store.Set([]byte(keyHex), bz)
}

// nullifierKey builds the key: {domain_bytes}/{scope_bytes}/{nullifier_hex}
func nullifierKey(domain uint64, scope uint64, nullifierHex string) []byte {
	key := uint64Bytes(domain)
	key = append(key, uint64Bytes(scope)...)
	return append(key, []byte(nullifierHex)...)
}

// uint64Bytes encodes a uint64 as big-endian bytes.
func uint64Bytes(v uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, v)
	return bz
}
