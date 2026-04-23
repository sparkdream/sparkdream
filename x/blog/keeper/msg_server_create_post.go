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

func (k msgServer) CreatePost(ctx context.Context, msg *types.MsgCreatePost) (*types.MsgCreatePostResponse, error) {
	// Validate creator address
	creatorAddrBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	creatorAddr := sdk.AccAddress(creatorAddrBytes)

	// Get params for validation
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Validate title
	if len(msg.Title) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "title cannot be empty")
	}
	if uint64(len(msg.Title)) > params.MaxTitleLength {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"title exceeds maximum length of %d characters", params.MaxTitleLength)
	}

	// Validate body
	if len(msg.Body) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "body cannot be empty")
	}
	if uint64(len(msg.Body)) > params.MaxBodyLength {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"body exceeds maximum length of %d characters", params.MaxBodyLength)
	}

	// Validate min_reply_trust_level
	if msg.MinReplyTrustLevel < -1 || msg.MinReplyTrustLevel > 4 {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"min_reply_trust_level must be between -1 and 4, got %d", msg.MinReplyTrustLevel)
	}

	// Rate limit check
	if err := k.checkRateLimit(ctx, "post", creatorAddr, params.MaxPostsPerDay); err != nil {
		return nil, err
	}

	// Charge cost_per_byte storage fee (applies to all posts, burned)
	if !params.CostPerByteExempt && params.CostPerByte.IsPositive() {
		contentBytes := int64(len(msg.Title)) + int64(len(msg.Body))
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

	// Determine TTL: active members get permanent posts, others get ephemeral
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	var expiresAt int64
	if k.isActiveMember(ctx, creatorAddr) {
		expiresAt = 0
	} else {
		expiresAt = sdkCtx.BlockTime().Unix() + params.EphemeralContentTtl
	}

	// Validate initiative reference before creating the post
	if msg.InitiativeId > 0 && k.repKeeper != nil {
		if err := k.repKeeper.ValidateInitiativeReference(ctx, msg.InitiativeId); err != nil {
			return nil, errorsmod.Wrapf(types.ErrInvalidInitiativeRef, "initiative %d: %s", msg.InitiativeId, err.Error())
		}
	}

	// Validate tags and bump usage metadata in the shared x/rep registry.
	if len(msg.Tags) > 0 {
		if err := k.validatePostTags(ctx, msg.Tags, sdkCtx.BlockTime().Unix()); err != nil {
			return nil, err
		}
	}

	post := types.Post{
		Creator:            msg.Creator,
		Title:              msg.Title,
		Body:               msg.Body,
		ContentType:        msg.ContentType,
		RepliesEnabled:     true,
		MinReplyTrustLevel: msg.MinReplyTrustLevel,
		CreatedAt:          sdkCtx.BlockTime().Unix(),
		Status:             types.PostStatus_POST_STATUS_ACTIVE,
		ExpiresAt:          expiresAt,
		FeeBytesHighWater:  uint64(len(msg.Title) + len(msg.Body)),
		InitiativeId:       msg.InitiativeId,
		Tags:               msg.Tags,
	}

	id := k.AppendPost(ctx, post)

	// Write tag → postID secondary index entries for ListPostsByTag.
	k.addTagIndexEntries(ctx, id, msg.Tags)

	// Create author bond if requested (requires repKeeper)
	if msg.AuthorBond != nil && msg.AuthorBond.IsPositive() && k.repKeeper != nil {
		if _, err := k.repKeeper.CreateAuthorBond(ctx, creatorAddr, reptypes.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, id, *msg.AuthorBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to create author bond")
		}
	}

	// Register initiative reference link for conviction propagation
	if msg.InitiativeId > 0 && k.repKeeper != nil {
		if err := k.repKeeper.RegisterContentInitiativeLink(ctx, msg.InitiativeId, int32(reptypes.StakeTargetType_STAKE_TARGET_BLOG_CONTENT), id); err != nil {
			return nil, errorsmod.Wrap(err, "failed to register content initiative link")
		}
	}

	// Add to expiry index if ephemeral
	if expiresAt > 0 {
		k.AddToExpiryIndex(ctx, expiresAt, "post", id)
	}

	// Increment rate limit
	k.incrementRateLimit(ctx, "post", creatorAddr)

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.post.created",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("creator", msg.Creator),
		sdk.NewAttribute("expires_at", fmt.Sprintf("%d", expiresAt)),
	))

	return &types.MsgCreatePostResponse{
		Id: id,
	}, nil
}
