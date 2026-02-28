package keeper

import (
	"context"
	"encoding/binary"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"

	"sparkdream/x/forum/types"
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

// SetAnonymousPostMeta stores anonymous post metadata.
func (k Keeper) SetAnonymousPostMeta(ctx context.Context, postId uint64, meta types.AnonymousPostMetadata) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonMetaPostKey))
	b := k.cdc.MustMarshal(&meta)
	store.Set(GetPostIDBytes(postId), b)
}

// GetAnonymousPostMeta retrieves anonymous post metadata.
func (k Keeper) GetAnonymousPostMeta(ctx context.Context, postId uint64) (types.AnonymousPostMetadata, bool) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonMetaPostKey))
	b := store.Get(GetPostIDBytes(postId))
	if b == nil {
		return types.AnonymousPostMetadata{}, false
	}
	var meta types.AnonymousPostMetadata
	k.cdc.MustUnmarshal(b, &meta)
	return meta, true
}

// SetAnonymousReplyMeta stores anonymous reply metadata.
func (k Keeper) SetAnonymousReplyMeta(ctx context.Context, replyId uint64, meta types.AnonymousPostMetadata) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonMetaReplyKey))
	b := k.cdc.MustMarshal(&meta)
	store.Set(GetPostIDBytes(replyId), b)
}

// GetAnonymousReplyMeta retrieves anonymous reply metadata.
func (k Keeper) GetAnonymousReplyMeta(ctx context.Context, replyId uint64) (types.AnonymousPostMetadata, bool) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonMetaReplyKey))
	b := store.Get(GetPostIDBytes(replyId))
	if b == nil {
		return types.AnonymousPostMetadata{}, false
	}
	var meta types.AnonymousPostMetadata
	k.cdc.MustUnmarshal(b, &meta)
	return meta, true
}

// nullifierKey builds the key: {domain_bytes}/{scope_bytes}/{nullifier_hex}
func nullifierKey(domain uint64, scope uint64, nullifierHex string) []byte {
	key := GetPostIDBytes(domain)
	key = append(key, GetPostIDBytes(scope)...)
	return append(key, []byte(nullifierHex)...)
}

// GetPostIDBytes returns the byte representation of the ID (big-endian uint64).
func GetPostIDBytes(id uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, id)
	return bz
}

// ExportAnonymousPostMeta exports all anonymous post metadata for genesis.
func (k Keeper) ExportAnonymousPostMeta(ctx context.Context) []types.AnonymousPostMetadata {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonMetaPostKey))
	var result []types.AnonymousPostMetadata
	iter := store.Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var meta types.AnonymousPostMetadata
		k.cdc.MustUnmarshal(iter.Value(), &meta)
		result = append(result, meta)
	}
	return result
}

// ExportAnonymousReplyMeta exports all anonymous reply metadata for genesis.
func (k Keeper) ExportAnonymousReplyMeta(ctx context.Context) []types.AnonymousPostMetadata {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonMetaReplyKey))
	var result []types.AnonymousPostMetadata
	iter := store.Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var meta types.AnonymousPostMetadata
		k.cdc.MustUnmarshal(iter.Value(), &meta)
		result = append(result, meta)
	}
	return result
}

// ExportNullifiers exports all nullifiers for genesis.
func (k Keeper) ExportNullifiers(ctx context.Context) []types.GenesisNullifierEntry {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonNullifierKey))
	var result []types.GenesisNullifierEntry
	iter := store.Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var entry types.AnonNullifierEntry
		k.cdc.MustUnmarshal(iter.Value(), &entry)
		key := iter.Key()
		// Key format: {domain_bytes(8)}{scope_bytes(8)}{nullifier_hex_string}
		if len(key) > 16 {
			domain := binary.BigEndian.Uint64(key[:8])
			scope := binary.BigEndian.Uint64(key[8:16])
			nullifierHex := string(key[16:])
			result = append(result, types.GenesisNullifierEntry{
				Domain:       domain,
				Scope:        scope,
				NullifierHex: nullifierHex,
				Entry:        &entry,
			})
		}
	}
	return result
}
