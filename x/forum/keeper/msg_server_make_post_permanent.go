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
// slot from a dedicated per-day MakePermanent rate-limit counter
// (params.MaxMakePermanentPerDay), independent of DailyPostLimit.
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

	// Dedicated MakePermanent daily counter — independent of DailyPostLimit.
	if err := k.checkAndUpdateMakePermanentRateLimit(ctx, msg.Creator, now, params.MaxMakePermanentPerDay); err != nil {
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
	// Record the promoter so ExpireHiddenPosts can issue a MemberWarning
	// against them if the post is later hidden and unappealed. Skip when the
	// promoter is the post author — promoting one's own post is not a
	// vouching act on someone else's content.
	if msg.Creator != post.Author {
		post.PromotedBy = msg.Creator
		post.PromotedAt = now
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
