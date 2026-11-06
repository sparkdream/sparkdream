package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PinPost pins a root post (thread) to the top of its category.
// Only governance authority can pin root posts.
func (k msgServer) PinPost(ctx context.Context, msg *types.MsgPinPost) (*types.MsgPinPostResponse, error) {
	if err := k.requireForumNotPaused(ctx); err != nil {
		return nil, err
	}
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Only governance, council, or operations committee can pin root posts
	if !k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority, "only governance, council, or operations committee can pin posts")
	}

	// Load post
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	// Verify this is a root post (thread)
	if post.ParentId != 0 {
		return nil, errorsmod.Wrap(types.ErrNotRootPost, "can only pin root posts (threads)")
	}

	// Check post is not deleted or hidden
	if post.Status == types.PostStatus_POST_STATUS_DELETED || post.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrapf(types.ErrPostStatus, "cannot pin post with status %s", post.Status.String())
	}

	// Pin is display-only ("featuring") and requires the post to already be
	// permanent. The lifecycle change (ephemeral → permanent) belongs to
	// MsgMakePostPermanent — see the rework notes on errors.ErrCannotPinEphemeral.
	if post.ExpirationTime > 0 {
		return nil, errorsmod.Wrapf(types.ErrCannotPinEphemeral, "post %d is ephemeral; call MakePostPermanent first", msg.PostId)
	}

	// Check if already pinned
	if post.Pinned {
		return nil, errorsmod.Wrap(types.ErrAlreadyPinned, "post is already pinned")
	}

	// A category holds at most max_pinned_per_category pinned threads
	// (spec §6, MsgPinPost). Counted from the pinned index, which every
	// pin and unpin keeps current.
	pinned, err := k.countPinnedInCategory(ctx, post.CategoryId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to count pinned posts")
	}
	if pinned >= types.DefaultMaxPinnedPerCategory {
		return nil, errorsmod.Wrapf(types.ErrMaxPinnedPosts, "category %d holds at most %d pinned posts", post.CategoryId, types.DefaultMaxPinnedPerCategory)
	}

	// Pin the post
	post.Pinned = true
	post.PinnedBy = msg.Creator
	post.PinnedAt = now
	post.PinPriority = msg.Priority

	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// FORUM-S2-8: index pinned posts by category for the PinnedPosts query.
	if err := k.PostsByPinned.Set(ctx, collections.Join(post.CategoryId, post.PostId)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update pinned index")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_pinned",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("pinned_by", msg.Creator),
			sdk.NewAttribute("priority", fmt.Sprintf("%d", msg.Priority)),
		),
	)

	return &types.MsgPinPostResponse{}, nil
}

// countPinnedInCategory returns the number of pinned root posts the
// PostsByPinned index holds for a category. The walk is bounded by the
// per-category pin cap plus any surplus a thread move brought in
// (MsgMoveThread re-keys a pinned post into its new category without
// re-checking the cap), so it stays a handful of keys.
func (k Keeper) countPinnedInCategory(ctx context.Context, categoryID uint64) (uint64, error) {
	var n uint64
	err := k.PostsByPinned.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](categoryID),
		func(_ collections.Pair[uint64, uint64]) (bool, error) {
			n++
			return false, nil
		})
	return n, err
}
