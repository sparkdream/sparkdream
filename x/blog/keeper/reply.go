package keeper

import (
	"context"
	"encoding/binary"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"

	"sparkdream/x/blog/types"
)

// AppendReply creates a new reply with auto-incremented ID.
func (k Keeper) AppendReply(ctx context.Context, reply types.Reply) uint64 {
	count := k.GetReplyCount(ctx)
	reply.Id = count
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ReplyKey))
	appendedValue := k.cdc.MustMarshal(&reply)
	store.Set(GetReplyIDBytes(reply.Id), appendedValue)

	// Add to post index
	postStore := prefix.NewStore(storeAdapter, []byte(types.ReplyPostKey))
	postKey := append(GetPostIDBytes(reply.PostId), GetReplyIDBytes(reply.Id)...)
	postStore.Set(postKey, []byte{0x01})

	k.SetReplyCount(ctx, count+1)
	return count
}

// SetReply stores/updates a reply at its current ID.
func (k Keeper) SetReply(ctx context.Context, reply types.Reply) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ReplyKey))
	b := k.cdc.MustMarshal(&reply)
	store.Set(GetReplyIDBytes(reply.Id), b)
}

// GetReply retrieves a reply by ID.
func (k Keeper) GetReply(ctx context.Context, id uint64) (val types.Reply, found bool) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ReplyKey))
	b := store.Get(GetReplyIDBytes(id))
	if b == nil {
		return val, false
	}
	k.cdc.MustUnmarshal(b, &val)
	return val, true
}

// RemoveReply deletes a reply record from the store.
func (k Keeper) RemoveReply(ctx context.Context, id uint64) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ReplyKey))
	store.Delete(GetReplyIDBytes(id))
}

// GetReplyCount returns the current reply counter.
func (k Keeper) GetReplyCount(ctx context.Context) uint64 {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte{})
	byteKey := []byte(types.ReplyCountKey)
	bz := store.Get(byteKey)
	if bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

// SetReplyCount updates the reply counter.
func (k Keeper) SetReplyCount(ctx context.Context, count uint64) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte{})
	byteKey := []byte(types.ReplyCountKey)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, count)
	store.Set(byteKey, bz)
}

// GetReplyIDBytes returns the byte representation of a reply ID.
func GetReplyIDBytes(id uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, id)
	return bz
}
