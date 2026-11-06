package keeper

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"
)

// setReaction records address's reaction to a target in ReactionDedup and in
// its ReactionsByTarget reverse index.
func (k Keeper) setReaction(ctx context.Context, address string, targetType types.FlagTargetType, targetID uint64, value uint32) error {
	if err := k.ReactionDedup.Set(ctx, ReactionDedupCompositeKey(address, targetType, targetID), value); err != nil {
		return err
	}
	return k.ReactionsByTarget.Set(ctx, collections.Join(FlagCompositeKey(targetType, targetID), address))
}

// deleteTargetReactions drops every ReactionDedup marker on a deleted target
// through the ReactionsByTarget reverse index (no full scan).
func (k Keeper) deleteTargetReactions(ctx context.Context, targetType types.FlagTargetType, targetID uint64) {
	target := FlagCompositeKey(targetType, targetID)
	var keys []collections.Pair[string, string]
	if err := k.ReactionsByTarget.Walk(ctx, collections.NewPrefixedPairRange[string, string](target),
		func(key collections.Pair[string, string]) (bool, error) {
			keys = append(keys, key)
			return false, nil
		}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("deleteTargetReactions: ReactionsByTarget.Walk failed", "err", err)
	}
	for _, key := range keys {
		if err := k.ReactionDedup.Remove(ctx, ReactionDedupCompositeKey(key.K2(), targetType, targetID)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteTargetReactions: ReactionDedup.Remove failed", "err", err)
		}
		if err := k.ReactionsByTarget.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deleteTargetReactions: ReactionsByTarget.Remove failed", "err", err)
		}
	}
}

// parseReactionDedupKey splits "{address}/{targetType}/{targetID}" (bech32
// addresses contain no '/').
func parseReactionDedupKey(key string) (string, types.FlagTargetType, uint64, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 3 {
		return "", 0, 0, fmt.Errorf("malformed reaction dedup key %q", key)
	}
	tt, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil {
		return "", 0, 0, fmt.Errorf("malformed reaction dedup key %q: %w", key, err)
	}
	id, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return "", 0, 0, fmt.Errorf("malformed reaction dedup key %q: %w", key, err)
	}
	return parts[0], types.FlagTargetType(tt), id, nil
}

// collaboratorRange is the Collaborator key range "{collectionID}/..." for
// one collection: keys are "{collectionID}/{address}", and '0' is the byte
// after '/', so the range covers exactly this collection's records.
func collaboratorRange(collectionID uint64) *collections.Range[string] {
	return new(collections.Range[string]).
		StartInclusive(fmt.Sprintf("%d/", collectionID)).
		EndExclusive(fmt.Sprintf("%d0", collectionID))
}

// removeItemOnChainRef drops item's ItemsByOnChainRef entry, if any.
func (k Keeper) removeItemOnChainRef(ctx context.Context, item types.Item) {
	if item.OnChain != nil {
		if err := k.ItemsByOnChainRef.Remove(ctx, collections.Join(onChainRefKey(item.OnChain), item.Id)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("removeItemOnChainRef: ItemsByOnChainRef.Remove failed", "err", err)
		}
	}
}

// checkNotHidden refuses an item mutation when the collection, or the item
// (when given), is HIDDEN. Hidden content is the evidence the appeal jury
// judges; editing, removing, adding or reordering it mid-appeal would let an
// owner swap the evidence (the forum's EditPost refuses hidden posts too).
func checkNotHidden(coll types.Collection, item *types.Item) error {
	if coll.Status == types.CollectionStatus_COLLECTION_STATUS_HIDDEN {
		return types.ErrContentHidden
	}
	if item != nil && item.Status == types.ItemStatus_ITEM_STATUS_HIDDEN {
		return types.ErrContentHidden
	}
	return nil
}
