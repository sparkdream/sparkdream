package keeper

import (
	"context"
	"encoding/binary"
	"fmt"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"

	"sparkdream/x/blog/types"
)

// AddToExpiryIndex adds content to the expiry index.
func (k Keeper) AddToExpiryIndex(ctx context.Context, expiresAt int64, contentType string, id uint64) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ExpiryKey))
	key := expiryKey(expiresAt, contentType, id)
	store.Set(key, []byte{0x01})
}

// RemoveFromExpiryIndex removes content from the expiry index.
func (k Keeper) RemoveFromExpiryIndex(ctx context.Context, expiresAt int64, contentType string, id uint64) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ExpiryKey))
	key := expiryKey(expiresAt, contentType, id)
	store.Delete(key)
}

// TombstoneExpiredContent processes all content that has expired before the given timestamp.
// Iterates the ExpiryIndex prefix up to beforeTimestamp and tombstones or upgrades each entry.
func (k Keeper) TombstoneExpiredContent(ctx context.Context, beforeTimestamp int64) {
	k.processExpiredContent(ctx, beforeTimestamp)
}

// expiryKey builds the key: {expires_at_bytes}/{type}/{id_bytes}
func expiryKey(expiresAt int64, contentType string, id uint64) []byte {
	// Use big-endian for timestamp to preserve sort order
	tsBz := make([]byte, 8)
	binary.BigEndian.PutUint64(tsBz, uint64(expiresAt))
	key := tsBz
	key = append(key, []byte(fmt.Sprintf("/%s/", contentType))...)
	idBz := make([]byte, 8)
	binary.BigEndian.PutUint64(idBz, id)
	return append(key, idBz...)
}
