package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MakePostPermanent promotes an ephemeral post to permanent by clearing
// expires_at and dropping its expiry-index entries. Idempotent on already-
// permanent posts (returns success without state change). Pin markers are
// untouched. Gated on params.make_permanent_min_trust_level and consumes one
// slot from the dedicated per-day MakePermanent rate-limit counter (shared
// with MsgMakeReplyPermanent and independent of MaxPinsPerDay /
// MaxPostsPerDay).
func (k msgServer) MakePostPermanent(ctx context.Context, msg *types.MsgMakePostPermanent) (*types.MsgMakePostPermanentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	post, found := k.GetPost(ctx, msg.Id)
	if !found {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.Id))
	}
	if post.Status == types.PostStatus_POST_STATUS_DELETED {
		return nil, errorsmod.Wrap(types.ErrPostDeleted, fmt.Sprintf("post %d has been deleted", msg.Id))
	}
	if post.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrPostHidden, fmt.Sprintf("post %d is hidden", msg.Id))
	}
	// Expired-but-not-yet-tombstoned content would race the next EndBlocker.
	if post.ExpiresAt > 0 && post.ExpiresAt <= sdkCtx.BlockTime().Unix() {
		return nil, errorsmod.Wrap(types.ErrPostExpired, fmt.Sprintf("post %d has expired", msg.Id))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if !k.meetsReplyTrustLevel(ctx, creatorAddr, int32(params.MakePermanentMinTrustLevel)) {
		return nil, k.trustLevelError(ctx, creatorAddr, int32(params.MakePermanentMinTrustLevel), "making posts permanent")
	}

	if err := k.checkRateLimit(ctx, "make_permanent", creatorAddr, params.MaxMakePermanentPerDay); err != nil {
		return nil, err
	}

	// Idempotent: already-permanent posts no-op (still consumes a rate-limit slot).
	if post.ExpiresAt == 0 {
		k.incrementRateLimit(ctx, "make_permanent", creatorAddr)
		return &types.MsgMakePostPermanentResponse{}, nil
	}

	oldExpiresAt := post.ExpiresAt
	post.ExpiresAt = 0
	if post.ConvictionSustained {
		post.ConvictionSustained = false
	}
	k.SetPost(ctx, post)
	k.RemoveFromExpiryIndex(ctx, oldExpiresAt, "post", post.Id)
	k.RemoveEphemeralAuthorIndex(ctx, post.Creator, EphemeralKindPost, post.Id)

	k.incrementRateLimit(ctx, "make_permanent", creatorAddr)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.post.upgraded",
		sdk.NewAttribute("id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("creator", post.Creator),
		sdk.NewAttribute("by", msg.Creator),
		sdk.NewAttribute("via", "make_permanent"),
	))

	return &types.MsgMakePostPermanentResponse{}, nil
}
