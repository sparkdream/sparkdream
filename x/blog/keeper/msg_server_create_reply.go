package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) CreateReply(ctx context.Context, msg *types.MsgCreateReply) (*types.MsgCreateReplyResponse, error) {
	creatorAddrBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	creatorAddr := sdk.AccAddress(creatorAddrBytes)

	// Get post, must exist and be active
	post, found := k.GetPost(ctx, msg.PostId)
	if !found {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d doesn't exist", msg.PostId))
	}
	if post.Status == types.PostStatus_POST_STATUS_DELETED {
		return nil, errorsmod.Wrap(types.ErrPostDeleted, "post has been deleted")
	}
	if post.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrPostHidden, "post is hidden")
	}

	// Check if replies are enabled
	if !post.RepliesEnabled {
		return nil, errorsmod.Wrap(types.ErrRepliesDisabled, "replies are disabled for this post")
	}

	// Get params for validation
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Validate body
	if len(msg.Body) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "body cannot be empty")
	}
	if uint64(len(msg.Body)) > params.MaxReplyLength {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"body exceeds maximum length of %d characters", params.MaxReplyLength)
	}

	// Trust level gate
	if !k.meetsReplyTrustLevel(ctx, creatorAddr, post.MinReplyTrustLevel) {
		return nil, errorsmod.Wrap(types.ErrInsufficientTrustLevel, "does not meet minimum trust level for replies on this post")
	}

	// Handle parent reply (nested replies)
	depth := uint32(0)
	if msg.ParentReplyId > 0 {
		parentReply, parentFound := k.GetReply(ctx, msg.ParentReplyId)
		if !parentFound {
			return nil, errorsmod.Wrap(types.ErrReplyNotFound, fmt.Sprintf("parent reply %d doesn't exist", msg.ParentReplyId))
		}
		if parentReply.PostId != msg.PostId {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "parent reply belongs to a different post")
		}
		if parentReply.Status == types.ReplyStatus_REPLY_STATUS_DELETED {
			return nil, errorsmod.Wrap(types.ErrReplyDeleted, "parent reply has been deleted")
		}
		if parentReply.Status == types.ReplyStatus_REPLY_STATUS_HIDDEN {
			return nil, errorsmod.Wrap(types.ErrReplyHidden, "parent reply is hidden")
		}
		depth = parentReply.Depth + 1
		if depth > params.MaxReplyDepth {
			return nil, errorsmod.Wrapf(types.ErrMaxReplyDepth, "depth %d exceeds max %d", depth, params.MaxReplyDepth)
		}
	}

	// Rate limit check
	if err := k.checkRateLimit(ctx, "reply", creatorAddr, params.MaxRepliesPerDay); err != nil {
		return nil, err
	}

	// Charge cost_per_byte storage fee
	if !params.CostPerByteExempt && params.CostPerByte.IsPositive() {
		contentBytes := int64(len(msg.Body))
		storageFee := sdk.NewCoin(params.CostPerByte.Denom,
			params.CostPerByte.Amount.MulRaw(contentBytes))
		if storageFee.IsPositive() {
			if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, sdk.NewCoins(storageFee)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to charge storage fee")
			}
			if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(storageFee)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to burn storage fee")
			}
		}
	}

	// Determine TTL: active members get permanent replies, others get ephemeral
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	var expiresAt int64
	if k.isActiveMember(ctx, creatorAddr) {
		expiresAt = 0
	} else {
		expiresAt = sdkCtx.BlockTime().Unix() + params.EphemeralContentTtl
	}

	reply := types.Reply{
		PostId:            msg.PostId,
		ParentReplyId:     msg.ParentReplyId,
		Creator:           msg.Creator,
		Body:              msg.Body,
		ContentType:       msg.ContentType,
		CreatedAt:         sdkCtx.BlockTime().Unix(),
		Depth:             depth,
		Status:            types.ReplyStatus_REPLY_STATUS_ACTIVE,
		ExpiresAt:         expiresAt,
		FeeBytesHighWater: uint64(len(msg.Body)),
	}

	id := k.AppendReply(ctx, reply)

	// Create author bond if requested (requires repKeeper)
	if msg.AuthorBond != nil && msg.AuthorBond.IsPositive() && k.repKeeper != nil {
		if _, err := k.repKeeper.CreateAuthorBond(ctx, creatorAddr, reptypes.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, id, *msg.AuthorBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to create author bond")
		}
	}

	// Increment post reply count
	post.ReplyCount++
	k.SetPost(ctx, post)

	// Add to expiry index if ephemeral
	if expiresAt > 0 {
		k.AddToExpiryIndex(ctx, expiresAt, "reply", id)
	}

	// Increment rate limit
	k.incrementRateLimit(ctx, "reply", creatorAddr)

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.reply.created",
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
		sdk.NewAttribute("creator", msg.Creator),
		sdk.NewAttribute("parent_reply_id", fmt.Sprintf("%d", msg.ParentReplyId)),
		sdk.NewAttribute("expires_at", fmt.Sprintf("%d", expiresAt)),
	))

	return &types.MsgCreateReplyResponse{
		Id: id,
	}, nil
}
