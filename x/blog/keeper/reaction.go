package keeper

import (
	"context"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"

	"sparkdream/x/blog/types"
)

// SetReaction stores a reaction record.
func (k Keeper) SetReaction(ctx context.Context, reaction types.Reaction) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ReactionKey))
	key := reactionKey(reaction.PostId, reaction.ReplyId, reaction.Creator)
	b := k.cdc.MustMarshal(&reaction)
	store.Set(key, b)

	// Update creator index
	creatorStore := prefix.NewStore(storeAdapter, []byte(types.ReactionCreatorKey))
	creatorKey := reactionCreatorKey(reaction.Creator, reaction.PostId, reaction.ReplyId)
	creatorStore.Set(creatorKey, []byte{0x01})
}

// GetReaction retrieves a user's reaction on a target.
func (k Keeper) GetReaction(ctx context.Context, postId uint64, replyId uint64, creator string) (val types.Reaction, found bool) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ReactionKey))
	key := reactionKey(postId, replyId, creator)
	b := store.Get(key)
	if b == nil {
		return val, false
	}
	k.cdc.MustUnmarshal(b, &val)
	return val, true
}

// RemoveReaction deletes a reaction record.
func (k Keeper) RemoveReaction(ctx context.Context, postId uint64, replyId uint64, creator string) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ReactionKey))
	key := reactionKey(postId, replyId, creator)
	store.Delete(key)

	// Remove creator index
	creatorStore := prefix.NewStore(storeAdapter, []byte(types.ReactionCreatorKey))
	creatorKey := reactionCreatorKey(creator, postId, replyId)
	creatorStore.Delete(creatorKey)
}

// GetReactionCounts retrieves aggregate counts for a target.
func (k Keeper) GetReactionCounts(ctx context.Context, postId uint64, replyId uint64) types.ReactionCounts {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ReactionCountKey))
	key := reactionCountKey(postId, replyId)
	b := store.Get(key)
	if b == nil {
		return types.ReactionCounts{}
	}
	var counts types.ReactionCounts
	k.cdc.MustUnmarshal(b, &counts)
	return counts
}

// SetReactionCounts stores aggregate counts for a target.
func (k Keeper) SetReactionCounts(ctx context.Context, postId uint64, replyId uint64, counts types.ReactionCounts) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ReactionCountKey))
	key := reactionCountKey(postId, replyId)
	b := k.cdc.MustMarshal(&counts)
	store.Set(key, b)
}

// RemoveReactionsForContent deletes all individual Reaction records and the
// aggregate ReactionCounts for a given (postId, replyId) target. For post-level
// reactions, pass replyId=0. This prevents orphaned reaction data after tombstoning.
func (k Keeper) RemoveReactionsForContent(ctx context.Context, postId uint64, replyId uint64) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))

	// Delete aggregate counts
	countStore := prefix.NewStore(storeAdapter, []byte(types.ReactionCountKey))
	countStore.Delete(reactionCountKey(postId, replyId))

	// Delete individual reactions by using a sub-prefixed store.
	// Key layout within ReactionKey store: {postId 8 bytes}{replyId 8 bytes}{creator...}
	// We create a sub-prefix store scoped to {postId}{replyId} to iterate only matching reactions.
	targetPrefix := reactionCountKey(postId, replyId) // 16 bytes: {postId}{replyId}
	reactionStore := prefix.NewStore(storeAdapter, append([]byte(types.ReactionKey), targetPrefix...))
	iter := reactionStore.Iterator(nil, nil)
	defer iter.Close()

	creatorStore := prefix.NewStore(storeAdapter, []byte(types.ReactionCreatorKey))
	for ; iter.Valid(); iter.Next() {
		// The key within the sub-prefixed store is just the creator string
		creator := string(iter.Key())
		// Clean up creator index
		creatorStore.Delete(reactionCreatorKey(creator, postId, replyId))
		reactionStore.Delete(iter.Key())
	}
}

// reactionKey builds the key for a specific reaction: {post_id}/{reply_id}/{creator}
func reactionKey(postId uint64, replyId uint64, creator string) []byte {
	key := append(GetPostIDBytes(postId), GetReplyIDBytes(replyId)...)
	return append(key, []byte(creator)...)
}

// reactionCountKey builds the key for reaction counts: {post_id}/{reply_id}
func reactionCountKey(postId uint64, replyId uint64) []byte {
	return append(GetPostIDBytes(postId), GetReplyIDBytes(replyId)...)
}

// reactionCreatorKey builds the creator index key: {creator}/{post_id}/{reply_id}
func reactionCreatorKey(creator string, postId uint64, replyId uint64) []byte {
	key := []byte(creator)
	key = append(key, GetPostIDBytes(postId)...)
	return append(key, GetReplyIDBytes(replyId)...)
}
