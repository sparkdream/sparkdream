package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) PinPost(ctx context.Context, msg *types.MsgPinPost) (*types.MsgPinPostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get post, must exist and be active
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

	// Must be ephemeral
	if post.ExpiresAt == 0 {
		return nil, errorsmod.Wrap(types.ErrContentNotEphemeral, "post is not ephemeral")
	}

	// Must not be expired
	if post.ExpiresAt <= sdkCtx.BlockTime().Unix() {
		return nil, errorsmod.Wrap(types.ErrPostExpired, fmt.Sprintf("post %d has expired", msg.Id))
	}

	// Must not already be pinned
	if post.PinnedBy != "" {
		return nil, errorsmod.Wrap(types.ErrAlreadyPinned, fmt.Sprintf("post %d is already pinned", msg.Id))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Creator must meet pin trust level
	if !k.meetsReplyTrustLevel(ctx, creatorAddr, int32(params.PinMinTrustLevel)) {
		return nil, errorsmod.Wrap(types.ErrInsufficientTrustLevel, "does not meet pin trust level requirement")
	}

	// Rate limit check
	if err := k.checkRateLimit(ctx, "pin", creatorAddr, params.MaxPinsPerDay); err != nil {
		return nil, err
	}

	// Remove from expiry index
	k.RemoveFromExpiryIndex(ctx, post.ExpiresAt, "post", post.Id)

	// Update post
	post.ExpiresAt = 0
	post.PinnedBy = msg.Creator
	post.PinnedAt = sdkCtx.BlockTime().Unix()
	if post.ConvictionSustained {
		post.ConvictionSustained = false
	}
	k.SetPost(ctx, post)

	// Increment rate limit
	k.incrementRateLimit(ctx, "pin", creatorAddr)

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.post.pinned",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("pinned_by", msg.Creator),
	))

	return &types.MsgPinPostResponse{}, nil
}
