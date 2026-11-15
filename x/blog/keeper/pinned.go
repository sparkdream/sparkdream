package keeper

import (
	"context"
	"encoding/binary"
	"math"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"

	"sparkdream/x/blog/types"
)

// pinnedPostIndexKey orders the index newest pin first: the leading 8 bytes
// are MaxInt64 - pinned_at, the trailing 8 the post id.
func pinnedPostIndexKey(pinnedAt int64, postID uint64) []byte {
	key := make([]byte, 16)
	binary.BigEndian.PutUint64(key[:8], uint64(math.MaxInt64-max(pinnedAt, 0)))
	binary.BigEndian.PutUint64(key[8:], postID)
	return key
}

func (k Keeper) pinnedPostStore(ctx context.Context) prefix.Store {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	return prefix.NewStore(storeAdapter, []byte(types.PinnedPostKey))
}

// addPinnedPostIndex indexes a post under its current pinned_at.
func (k Keeper) addPinnedPostIndex(ctx context.Context, post types.Post) {
	k.pinnedPostStore(ctx).Set(pinnedPostIndexKey(post.PinnedAt, post.Id), []byte{0x01})
}

// removePinnedPostIndex drops a post's entry; call it before pinned_at is
// cleared, since the key is derived from it.
func (k Keeper) removePinnedPostIndex(ctx context.Context, post types.Post) {
	k.pinnedPostStore(ctx).Delete(pinnedPostIndexKey(post.PinnedAt, post.Id))
}

// RebuildPinnedPostIndex clears the pinned post index and rebuilds it from
// the stored posts. It is the v1 -> v2 migration: chains that pinned posts
// before the index existed have none of them indexed.
func (k Keeper) RebuildPinnedPostIndex(ctx context.Context) error {
	idx := k.pinnedPostStore(ctx)
	var stale [][]byte
	it := idx.Iterator(nil, nil)
	for ; it.Valid(); it.Next() {
		stale = append(stale, append([]byte{}, it.Key()...))
	}
	if err := it.Close(); err != nil {
		return err
	}
	for _, key := range stale {
		idx.Delete(key)
	}

	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	postIter := prefix.NewStore(storeAdapter, []byte(types.PostKey)).Iterator(nil, nil)
	defer postIter.Close()
	for ; postIter.Valid(); postIter.Next() {
		var post types.Post
		if err := k.cdc.Unmarshal(postIter.Value(), &post); err != nil {
			return err
		}
		if post.PinnedBy != "" {
			idx.Set(pinnedPostIndexKey(post.PinnedAt, post.Id), []byte{0x01})
		}
	}
	return nil
}
