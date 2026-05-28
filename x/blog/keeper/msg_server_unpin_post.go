package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UnpinPost clears the pinned marker on a post without re-introducing an
// expiry. Once a post is permanent (either pinned or authored as a member),
// unpin only walks back the curation marker; the content stays live. Gated on
// the same trust level as Pin and shares its rate-limit counter.
func (k msgServer) UnpinPost(ctx context.Context, msg *types.MsgUnpinPost) (*types.MsgUnpinPostResponse, error) {
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

	if post.PinnedBy == "" {
		return nil, errorsmod.Wrap(types.ErrNotPinned, fmt.Sprintf("post %d is not pinned", msg.Id))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if !k.meetsReplyTrustLevel(ctx, creatorAddr, int32(params.PinMinTrustLevel)) {
		return nil, k.trustLevelError(ctx, creatorAddr, int32(params.PinMinTrustLevel), "unpinning posts")
	}

	if err := k.checkRateLimit(ctx, "pin", creatorAddr, params.MaxPinsPerDay); err != nil {
		return nil, err
	}

	prevPinnedBy := post.PinnedBy
	post.PinnedBy = ""
	post.PinnedAt = 0
	k.SetPost(ctx, post)

	k.incrementRateLimit(ctx, "pin", creatorAddr)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.post.unpinned",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("unpinned_by", msg.Creator),
		sdk.NewAttribute("was_pinned_by", prevPinnedBy),
	))

	return &types.MsgUnpinPostResponse{}, nil
}
