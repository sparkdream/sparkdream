package keeper

import (
	"context"
	"fmt"
	commontypes "sparkdream/x/common/types"

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

	// Pin is display-only and requires the post to already be permanent.
	// Promote ephemeral content with MakePostPermanent first (strict
	// separation between the lifecycle change and the display marker).
	if post.ExpiresAt > 0 {
		return nil, errorsmod.Wrap(types.ErrCannotPinEphemeral, fmt.Sprintf("post %d is ephemeral; call MakePostPermanent first", msg.Id))
	}

	if post.PinnedBy != "" {
		return nil, errorsmod.Wrap(types.ErrAlreadyPinned, fmt.Sprintf("post %d is already pinned", msg.Id))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Creator must meet pin trust level (helper distinguishes ErrNotMember
	// from ErrInsufficientTrustLevel so the caller knows whether the fix is
	// to join or to earn trust)
	if !k.meetsReplyTrustLevel(ctx, creatorAddr, commontypes.Saturate[int32](params.PinMinTrustLevel)) {
		return nil, k.trustLevelError(ctx, creatorAddr, commontypes.Saturate[int32](params.PinMinTrustLevel), "pinning posts")
	}

	if err := k.checkRateLimit(ctx, "pin", creatorAddr, params.MaxPinsPerDay); err != nil {
		return nil, err
	}

	post.PinnedBy = msg.Creator
	post.PinnedAt = sdkCtx.BlockTime().Unix()
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
