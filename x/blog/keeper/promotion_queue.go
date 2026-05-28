package keeper

import (
	"context"
	"encoding/binary"
	"fmt"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/blog/types"
)

const (
	EphemeralKindPost  byte = 1
	EphemeralKindReply byte = 2
)

// blogModuleAddrString is the bech32 address of the blog module account, used
// to distinguish anonymous content (creator = module addr) from authored
// content. Anonymous ephemerals are skipped by the EphemeralByAuthor index and
// the promotion queue — they cannot "become a member" and have their own
// conviction-renewal lifecycle.
func (k Keeper) blogModuleAddrString() string {
	return authtypes.NewModuleAddress(types.ModuleName).String()
}

// AddEphemeralAuthorIndex records that `creator` has an ephemeral post/reply
// (`kind`, `id`) pending eventual promotion. No-op for module-account
// (anonymous) creators.
func (k Keeper) AddEphemeralAuthorIndex(ctx context.Context, creator string, kind byte, id uint64) {
	if creator == "" || creator == k.blogModuleAddrString() {
		return
	}
	store := k.ephemeralByAuthorStore(ctx)
	store.Set(ephemeralAuthorKey(creator, kind, id), []byte{0x01})
}

// RemoveEphemeralAuthorIndex idempotently removes a (creator, kind, id) entry
// from the EphemeralByAuthor index.
func (k Keeper) RemoveEphemeralAuthorIndex(ctx context.Context, creator string, kind byte, id uint64) {
	if creator == "" {
		return
	}
	store := k.ephemeralByAuthorStore(ctx)
	store.Delete(ephemeralAuthorKey(creator, kind, id))
}

// EnqueueAuthorForPromotion adds `creator` to the promotion queue with the
// current block height stamped as the enqueue marker. Idempotent.
func (k Keeper) EnqueueAuthorForPromotion(ctx context.Context, creator string) {
	if creator == "" || creator == k.blogModuleAddrString() {
		return
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := k.promotionQueueStore(ctx)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, uint64(sdkCtx.BlockHeight()))
	store.Set([]byte(creator), bz)
}

// dequeueAuthorForPromotion removes `creator` from the promotion queue.
func (k Keeper) dequeueAuthorForPromotion(ctx context.Context, creator string) {
	store := k.promotionQueueStore(ctx)
	store.Delete([]byte(creator))
}

// IsAuthorQueuedForPromotion reports whether `creator` is currently in the
// promotion queue.
func (k Keeper) IsAuthorQueuedForPromotion(ctx context.Context, creator string) bool {
	store := k.promotionQueueStore(ctx)
	return store.Has([]byte(creator))
}

// drainPromotionQueue eagerly promotes up to `budget` ephemeral posts/replies
// to permanent across all queued authors, iterating the queue in
// bech32-string order. Authors whose ephemeral content is fully drained are
// removed from the queue. Returns the count of items actually promoted.
func (k Keeper) drainPromotionQueue(ctx context.Context, budget int) int {
	if budget <= 0 {
		return 0
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	queueStore := k.promotionQueueStore(ctx)
	iter := queueStore.Iterator(nil, nil)
	defer iter.Close()

	type queueEntry struct {
		creator string
	}
	var authors []queueEntry
	for ; iter.Valid(); iter.Next() {
		authors = append(authors, queueEntry{creator: string(iter.Key())})
	}
	// Iterator is exhausted; safe to mutate the store now.

	promoted := 0
	for _, entry := range authors {
		if promoted >= budget {
			return promoted
		}
		done := k.promoteAuthorEphemerals(ctx, sdkCtx, entry.creator, blockTime, &promoted, budget)
		if done {
			k.dequeueAuthorForPromotion(ctx, entry.creator)
		}
	}
	return promoted
}

// promoteAuthorEphemerals walks the EphemeralByAuthor prefix for `creator`
// and promotes each entry to permanent until either the per-block budget is
// exhausted or the author has no more ephemeral content. Returns true iff
// the author has been fully drained and can be removed from the queue.
func (k Keeper) promoteAuthorEphemerals(ctx context.Context, sdkCtx sdk.Context, creator string, blockTime int64, promoted *int, budget int) bool {
	store := k.ephemeralByAuthorStore(ctx)
	authorPrefix := append([]byte(creator), '/')
	authorStore := prefix.NewStore(store, authorPrefix)

	type ephemeralEntry struct {
		kind byte
		id   uint64
	}
	// Snapshot entries to avoid mutating the store mid-iteration.
	iter := authorStore.Iterator(nil, nil)
	var entries []ephemeralEntry
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		// Key format: {kind_byte}/{id_8B_BE}
		if len(key) < 1+1+8 || key[1] != '/' {
			continue
		}
		entries = append(entries, ephemeralEntry{
			kind: key[0],
			id:   binary.BigEndian.Uint64(key[2:]),
		})
	}
	iter.Close()

	for _, e := range entries {
		if *promoted >= budget {
			return false
		}
		switch e.kind {
		case EphemeralKindPost:
			k.promoteEphemeralPost(ctx, sdkCtx, creator, e.id, blockTime, promoted)
		case EphemeralKindReply:
			k.promoteEphemeralReply(ctx, sdkCtx, creator, e.id, blockTime, promoted)
		default:
			// Unknown kind — drop the orphan index entry.
			store.Delete(ephemeralAuthorKey(creator, e.kind, e.id))
		}
	}
	return true
}

// promoteEphemeralPost promotes a single ephemeral post identified by `id` to
// permanent. Handles the cases where the post is gone, already permanent, or
// in a non-promotable state by cleaning up the index entry.
func (k Keeper) promoteEphemeralPost(ctx context.Context, sdkCtx sdk.Context, creator string, id uint64, blockTime int64, promoted *int) {
	post, found := k.GetPost(ctx, id)
	if !found {
		k.RemoveEphemeralAuthorIndex(ctx, creator, EphemeralKindPost, id)
		return
	}
	// Author mismatch or already permanent / tombstoned — drop the stale index entry.
	if post.Creator != creator || post.ExpiresAt == 0 ||
		post.Status == types.PostStatus_POST_STATUS_DELETED {
		k.RemoveEphemeralAuthorIndex(ctx, creator, EphemeralKindPost, id)
		return
	}

	oldExpiresAt := post.ExpiresAt
	post.ExpiresAt = 0
	if post.ConvictionSustained {
		post.ConvictionSustained = false
	}
	k.SetPost(ctx, post)
	k.RemoveFromExpiryIndex(ctx, oldExpiresAt, "post", id)
	k.RemoveEphemeralAuthorIndex(ctx, creator, EphemeralKindPost, id)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"blog.post.upgraded",
		sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("creator", creator),
		sdk.NewAttribute("via", "promotion_queue"),
	))
	*promoted++
	_ = blockTime
}

// promoteEphemeralReply mirrors promoteEphemeralPost for replies.
func (k Keeper) promoteEphemeralReply(ctx context.Context, sdkCtx sdk.Context, creator string, id uint64, blockTime int64, promoted *int) {
	reply, found := k.GetReply(ctx, id)
	if !found {
		k.RemoveEphemeralAuthorIndex(ctx, creator, EphemeralKindReply, id)
		return
	}
	if reply.Creator != creator || reply.ExpiresAt == 0 ||
		reply.Status == types.ReplyStatus_REPLY_STATUS_DELETED {
		k.RemoveEphemeralAuthorIndex(ctx, creator, EphemeralKindReply, id)
		return
	}

	oldExpiresAt := reply.ExpiresAt
	reply.ExpiresAt = 0
	if reply.ConvictionSustained {
		reply.ConvictionSustained = false
	}
	k.SetReply(ctx, reply)
	k.RemoveFromExpiryIndex(ctx, oldExpiresAt, "reply", id)
	k.RemoveEphemeralAuthorIndex(ctx, creator, EphemeralKindReply, id)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"blog.reply.upgraded",
		sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", reply.PostId)),
		sdk.NewAttribute("creator", creator),
		sdk.NewAttribute("via", "promotion_queue"),
	))
	*promoted++
	_ = blockTime
}

func (k Keeper) ephemeralByAuthorStore(ctx context.Context) prefix.Store {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	return prefix.NewStore(storeAdapter, []byte(types.EphemeralByAuthorKey))
}

func (k Keeper) promotionQueueStore(ctx context.Context) prefix.Store {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	return prefix.NewStore(storeAdapter, []byte(types.PromotionQueueKey))
}

// ephemeralAuthorKey builds the EphemeralByAuthor entry key:
// {creator_bech32_bytes}/{kind_byte}/{id_8B_BE}
//
// bech32 is alphanumeric only, so '/' is a safe segment separator.
func ephemeralAuthorKey(creator string, kind byte, id uint64) []byte {
	key := append([]byte(creator), '/')
	key = append(key, kind, '/')
	idBz := make([]byte, 8)
	binary.BigEndian.PutUint64(idBz, id)
	return append(key, idBz...)
}
