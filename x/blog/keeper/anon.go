package keeper

import (
	"context"
	"encoding/binary"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"

	"sparkdream/x/blog/types"
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
	store.Set(GetReplyIDBytes(replyId), b)
}

// GetAnonymousReplyMeta retrieves anonymous reply metadata.
func (k Keeper) GetAnonymousReplyMeta(ctx context.Context, replyId uint64) (types.AnonymousPostMetadata, bool) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.AnonMetaReplyKey))
	b := store.Get(GetReplyIDBytes(replyId))
	if b == nil {
		return types.AnonymousPostMetadata{}, false
	}
	var meta types.AnonymousPostMetadata
	k.cdc.MustUnmarshal(b, &meta)
	return meta, true
}

// GetAnonSubsidyLastEpoch returns the last epoch for which anonymous subsidy was drawn.
func (k Keeper) GetAnonSubsidyLastEpoch(ctx context.Context) uint64 {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte{})
	b := store.Get([]byte(types.AnonSubsidyKey))
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// SetAnonSubsidyLastEpoch stores the last epoch for which anonymous subsidy was drawn.
func (k Keeper) SetAnonSubsidyLastEpoch(ctx context.Context, epoch uint64) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte{})
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, epoch)
	store.Set([]byte(types.AnonSubsidyKey), bz)
}

// nullifierKey builds the key: {domain_bytes}/{scope_bytes}/{nullifier_hex}
func nullifierKey(domain uint64, scope uint64, nullifierHex string) []byte {
	key := GetPostIDBytes(domain)
	key = append(key, GetPostIDBytes(scope)...)
	return append(key, []byte(nullifierHex)...)
}
