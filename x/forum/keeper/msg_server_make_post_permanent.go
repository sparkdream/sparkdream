package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

// MakePostPermanent promotes an ephemeral Post (root post or reply) to
// permanent by clearing ExpirationTime and dropping its ExpirationQueue /
// EphemeralByAuthor entries. Pin markers are untouched.
//
// Strict separation from Pin: pin is display-only and now refuses ephemeral
// targets; MakePostPermanent owns the lifecycle change. Gated on
// params.MakePermanentMinTrustLevel (default PROVISIONAL) and consumes one
// slot from the daily post-rate limit so it cannot be used to bypass the
// daily-post quota in bulk.
//
// Idempotent on already-permanent posts (returns success without state
// change) — this lets callers blindly upgrade-then-pin without first
// querying ExpirationTime.
func (k msgServer) MakePostPermanent(ctx context.Context, msg *types.MsgMakePostPermanent) (*types.MsgMakePostPermanentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}
	if post.Status == types.PostStatus_POST_STATUS_DELETED {
		return nil, errorsmod.Wrapf(types.ErrPostDeleted, "post %d has been deleted", msg.PostId)
	}
	if post.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrapf(types.ErrPostAlreadyHidden, "post %d is hidden", msg.PostId)
	}
	// Expired-but-not-yet-pruned content would race the next EndBlocker.
	if post.ExpirationTime > 0 && post.ExpirationTime <= now {
		return nil, errorsmod.Wrapf(types.ErrPostExpired, "post %d has expired", msg.PostId)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to load params")
	}

	// Trust-level gate (default PROVISIONAL).
	callerTrust := k.GetTrustLevel(ctx, msg.Creator)
	if callerTrust < uint64(params.MakePermanentMinTrustLevel) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
			"need trust level %s (got %s) to make a post permanent",
			reptypes.TrustLevel_name[int32(params.MakePermanentMinTrustLevel)],
			reptypes.TrustLevel_name[int32(callerTrust)])
	}

	// Consume one slot from the daily-post rate limit so MakePermanent calls
	// can't be used to bypass the per-day posting quota. This matches the
	// "one content action per slot" budgeting model used elsewhere in forum.
	if err := k.checkAndUpdateRateLimit(ctx, msg.Creator, now); err != nil {
		return nil, err
	}

	// Idempotent on already-permanent posts.
	if post.ExpirationTime == 0 {
		return &types.MsgMakePostPermanentResponse{}, nil
	}

	oldExpiresAt := post.ExpirationTime
	post.ExpirationTime = 0
	if post.ConvictionSustained {
		post.ConvictionSustained = false
	}
	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}
	_ = k.ExpirationQueue.Remove(ctx, collections.Join(oldExpiresAt, msg.PostId))
	_ = k.EphemeralByAuthor.Remove(ctx, collections.Join(post.Author, msg.PostId))

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("forum.post.upgraded",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
		sdk.NewAttribute("creator", post.Author),
		sdk.NewAttribute("by", msg.Creator),
		sdk.NewAttribute("via", "make_permanent"),
	))

	return &types.MsgMakePostPermanentResponse{}, nil
}
