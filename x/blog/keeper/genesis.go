package keeper

import (
	"context"
	"encoding/binary"
	"fmt"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"

	"sparkdream/x/blog/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	if err := genState.Params.Validate(); err != nil {
		return fmt.Errorf("invalid blog params in genesis: %w", err)
	}
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// Import posts
	for _, post := range genState.Posts {
		k.SetPost(ctx, post)
	}
	k.SetPostCount(ctx, genState.PostCount)

	// Import replies
	for _, reply := range genState.Replies {
		k.SetReply(ctx, reply)
	}
	k.SetReplyCount(ctx, genState.ReplyCount)

	// Import reactions
	for _, reaction := range genState.Reactions {
		k.SetReaction(ctx, reaction)
	}

	// Import reaction counts
	for _, rc := range genState.ReactionCounts {
		if rc.Counts != nil {
			k.SetReactionCounts(ctx, rc.PostId, rc.ReplyId, *rc.Counts)
		}
	}

	// Rebuild derived indexes that SetPost/SetReply don't create
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))

	// Rebuild creator post index and expiry index for posts
	creatorStore := prefix.NewStore(storeAdapter, []byte(types.CreatorPostKey))
	for _, post := range genState.Posts {
		creatorKey := append([]byte(post.Creator+"/"), GetPostIDBytes(post.Id)...)
		creatorStore.Set(creatorKey, []byte{0x01})

		if post.ExpiresAt > 0 {
			k.AddToExpiryIndex(ctx, post.ExpiresAt, "post", post.Id)
		}
	}

	// Rebuild reply post index and expiry index for replies
	postStore := prefix.NewStore(storeAdapter, []byte(types.ReplyPostKey))
	for _, reply := range genState.Replies {
		postKey := append(GetPostIDBytes(reply.PostId), GetReplyIDBytes(reply.Id)...)
		postStore.Set(postKey, []byte{0x01})

		if reply.ExpiresAt > 0 {
			k.AddToExpiryIndex(ctx, reply.ExpiresAt, "reply", reply.Id)
		}
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))

	// Export posts
	postStore := prefix.NewStore(storeAdapter, []byte(types.PostKey))
	postIter := postStore.Iterator(nil, nil)
	for ; postIter.Valid(); postIter.Next() {
		var post types.Post
		k.cdc.MustUnmarshal(postIter.Value(), &post)
		genesis.Posts = append(genesis.Posts, post)
	}
	postIter.Close()
	genesis.PostCount = k.GetPostCount(ctx)

	// Export replies
	replyStore := prefix.NewStore(storeAdapter, []byte(types.ReplyKey))
	replyIter := replyStore.Iterator(nil, nil)
	for ; replyIter.Valid(); replyIter.Next() {
		var reply types.Reply
		k.cdc.MustUnmarshal(replyIter.Value(), &reply)
		genesis.Replies = append(genesis.Replies, reply)
	}
	replyIter.Close()
	genesis.ReplyCount = k.GetReplyCount(ctx)

	// Export reactions
	reactionStore := prefix.NewStore(storeAdapter, []byte(types.ReactionKey))
	reactionIter := reactionStore.Iterator(nil, nil)
	for ; reactionIter.Valid(); reactionIter.Next() {
		var reaction types.Reaction
		k.cdc.MustUnmarshal(reactionIter.Value(), &reaction)
		genesis.Reactions = append(genesis.Reactions, reaction)
	}
	reactionIter.Close()

	// Export reaction counts
	countsStore := prefix.NewStore(storeAdapter, []byte(types.ReactionCountKey))
	countsIter := countsStore.Iterator(nil, nil)
	for ; countsIter.Valid(); countsIter.Next() {
		key := countsIter.Key()
		if len(key) < 16 {
			continue
		}
		postId := binary.BigEndian.Uint64(key[:8])
		replyId := binary.BigEndian.Uint64(key[8:16])
		var counts types.ReactionCounts
		k.cdc.MustUnmarshal(countsIter.Value(), &counts)
		genesis.ReactionCounts = append(genesis.ReactionCounts, types.GenesisReactionCounts{
			PostId:  postId,
			ReplyId: replyId,
			Counts:  &counts,
		})
	}
	countsIter.Close()

	return genesis, nil
}
