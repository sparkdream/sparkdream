package keeper

import (
	"context"
	"fmt"
	"strconv"

	"sparkdream/x/collect/types"
)

// onChainRefKey builds the composite key for the ItemsByOnChainRef index.
// Format: "{module}:{entity_type}:{entity_id}", e.g. "blog:post:42".
func onChainRefKey(ref *types.OnChainReference) string {
	return fmt.Sprintf("%s:%s:%s", ref.Module, ref.EntityType, ref.EntityId)
}

// validateOnChainReference checks that the referenced on-chain content exists.
// Dispatches to the appropriate keeper based on the module field.
// Unknown modules are allowed without validation (forward-compatible).
func (k Keeper) validateOnChainReference(ctx context.Context, ref *types.OnChainReference) error {
	switch ref.Module {
	case "blog":
		if k.blogKeeper == nil {
			return nil // skip validation if keeper not wired
		}
		id, err := strconv.ParseUint(ref.EntityId, 10, 64)
		if err != nil {
			return types.ErrInvalidOnChainRef
		}
		switch ref.EntityType {
		case "post":
			if !k.blogKeeper.HasPost(ctx, id) {
				return types.ErrOnChainRefNotFound
			}
		case "reply":
			if !k.blogKeeper.HasReply(ctx, id) {
				return types.ErrOnChainRefNotFound
			}
		default:
			return types.ErrInvalidOnChainRef
		}
	case "forum":
		if k.forumKeeper == nil {
			return nil
		}
		id, err := strconv.ParseUint(ref.EntityId, 10, 64)
		if err != nil {
			return types.ErrInvalidOnChainRef
		}
		// Forum replies are posts with ParentId > 0; both stored in Post collection
		switch ref.EntityType {
		case "post", "reply":
			if !k.forumKeeper.HasPost(ctx, id) {
				return types.ErrOnChainRefNotFound
			}
		default:
			return types.ErrInvalidOnChainRef
		}
	default:
		// Unknown module — allow without validation (forward-compatible)
	}
	return nil
}
