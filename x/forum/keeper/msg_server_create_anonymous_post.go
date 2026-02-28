package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

func (k msgServer) CreateAnonymousPost(ctx context.Context, msg *types.MsgCreateAnonymousPost) (*types.MsgCreateAnonymousPostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Submitter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid submitter address")
	}

	submitterAddr, _ := sdk.AccAddressFromBech32(msg.Submitter)
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Check forum not paused
	if params.ForumPaused {
		return nil, types.ErrForumPaused
	}

	// Check anonymous posting enabled
	if !params.AnonymousPostingEnabled {
		return nil, errorsmod.Wrap(types.ErrAnonPostingDisabled, "anonymous posting is not enabled")
	}

	// Check VoteKeeper available
	if k.voteKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrAnonPostingDisabled, "vote module not available")
	}

	// Validate content
	if msg.Content == "" {
		return nil, types.ErrEmptyContent
	}
	if uint64(len(msg.Content)) > params.MaxContentSize {
		return nil, errorsmod.Wrapf(types.ErrContentTooLarge, "content too long: %d > %d", len(msg.Content), params.MaxContentSize)
	}

	// Validate category exists and allows anonymous
	category, err := k.Category.Get(ctx, msg.CategoryId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCategoryNotFound, fmt.Sprintf("category %d not found", msg.CategoryId))
	}
	if category.AdminOnlyWrite {
		return nil, types.ErrAdminOnlyWrite
	}
	if !category.AllowAnonymous {
		return nil, errorsmod.Wrap(types.ErrCategoryAnonNotAllowed, "category does not allow anonymous posts")
	}

	// Validate min_trust_level meets the configured minimum
	if msg.MinTrustLevel < params.AnonymousMinTrustLevel {
		return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
			"min_trust_level %d below required %d", msg.MinTrustLevel, params.AnonymousMinTrustLevel)
	}

	// Validate merkle root against the current or previous trust tree root
	if k.repKeeper != nil {
		currentRoot, err := k.repKeeper.GetMemberTrustTreeRoot(ctx)
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrAnonPostingDisabled, "trust tree not available")
		}
		prevRoot := k.repKeeper.GetPreviousMemberTrustTreeRoot(ctx)
		if !bytes.Equal(msg.MerkleRoot, currentRoot) && !bytes.Equal(msg.MerkleRoot, prevRoot) {
			return nil, errorsmod.Wrap(types.ErrInvalidProof, "stale or invalid merkle root")
		}
	}

	// Compute epoch for nullifier scope
	epochDuration := DefaultEpochDuration
	if k.seasonKeeper != nil {
		epochDuration = k.seasonKeeper.GetEpochDuration(ctx)
	}
	epoch := uint64(sdkCtx.BlockTime().Unix()) / uint64(epochDuration)

	// Check nullifier not used (domain=3 for forum posts)
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	if k.IsNullifierUsed(ctx, 3, epoch, nullifierHex) {
		return nil, errorsmod.Wrap(types.ErrNullifierUsed, "nullifier already used in this epoch")
	}

	// Verify ZK proof
	if err := k.voteKeeper.VerifyAnonymousActionProof(ctx, msg.Proof, msg.Nullifier, msg.MerkleRoot, msg.MinTrustLevel); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidProof, err.Error())
	}

	// Validate initiative reference
	if msg.InitiativeId > 0 && k.repKeeper != nil {
		if err := k.repKeeper.ValidateInitiativeReference(ctx, msg.InitiativeId); err != nil {
			return nil, errorsmod.Wrapf(types.ErrInvalidInitiativeRef, "initiative %d: %s", msg.InitiativeId, err.Error())
		}
	}

	// Note: rate limiting is NOT applied to the relay address for anonymous posts.
	// The nullifier already prevents double-posting per epoch, and rate-limiting
	// the relay would create a privacy-leaking correlation vector.

	// Charge storage fee (with optional subsidy for approved relays)
	if !params.CostPerByteExempt && params.CostPerByte.IsPositive() {
		contentBytes := int64(len(msg.Content))
		storageFee := sdk.NewCoin(params.CostPerByte.Denom, params.CostPerByte.Amount.MulRaw(contentBytes))
		if storageFee.IsPositive() {
			// Try to subsidize the fee for approved relays
			subsidy := k.TrySubsidizeAnonymousAction(ctx, params, msg.Submitter, storageFee, epoch)
			netFee := storageFee.Sub(subsidy)
			if netFee.IsPositive() {
				if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, submitterAddr, types.ModuleName, sdk.NewCoins(netFee)); err != nil {
					return nil, err
				}
				if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(netFee)); err != nil {
					return nil, err
				}
			}
		}
	}

	// Validate tags
	now := sdkCtx.BlockTime().Unix()
	if len(msg.Tags) > 0 {
		if err := k.validatePostTags(ctx, msg.Tags, now); err != nil {
			return nil, err
		}
	}

	// Generate post ID
	postID, err := k.PostSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate post ID")
	}

	// Create post with module account as author (permanent, no expiry)
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	post := types.Post{
		PostId:      postID,
		CategoryId:  msg.CategoryId,
		RootId:      0,
		ParentId:    0,
		Author:      moduleAddr,
		Content:     msg.Content,
		CreatedAt:   now,
		Status:      types.PostStatus_POST_STATUS_ACTIVE,
		Tags:         msg.Tags,
		ContentType:  msg.ContentType,
		InitiativeId: msg.InitiativeId,
	}

	if err := k.Post.Set(ctx, postID, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store post")
	}

	// Store anonymous metadata
	k.SetAnonymousPostMeta(ctx, postID, types.AnonymousPostMetadata{
		ContentId:        postID,
		Nullifier:        msg.Nullifier,
		MerkleRoot:       msg.MerkleRoot,
		ProvenTrustLevel: msg.MinTrustLevel,
	})

	// Register initiative reference link for conviction propagation
	if msg.InitiativeId > 0 && k.repKeeper != nil {
		if err := k.repKeeper.RegisterContentInitiativeLink(ctx, msg.InitiativeId, int32(reptypes.StakeTargetType_STAKE_TARGET_FORUM_CONTENT), postID); err != nil {
			return nil, errorsmod.Wrap(err, "failed to register content initiative link")
		}
	}

	// Record nullifier (domain=3 for forum posts)
	k.SetNullifierUsed(ctx, 3, epoch, nullifierHex, types.AnonNullifierEntry{
		UsedAt: now,
		Domain: 3,
		Scope:  epoch,
	})

	// Emit standard post_created event for indexer compatibility (author = module account)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("post_created",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
		sdk.NewAttribute("category_id", fmt.Sprintf("%d", msg.CategoryId)),
		sdk.NewAttribute("parent_id", "0"),
		sdk.NewAttribute("author", moduleAddr),
		sdk.NewAttribute("is_ephemeral", "false"),
		sdk.NewAttribute("is_anonymous", "true"),
	))

	// Emit anonymous-specific event (no submitter — preserve relay anonymity)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("forum.anonymous_post.created",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
		sdk.NewAttribute("category_id", fmt.Sprintf("%d", msg.CategoryId)),
		sdk.NewAttribute("proven_trust_level", fmt.Sprintf("%d", msg.MinTrustLevel)),
		sdk.NewAttribute("nullifier_hex", nullifierHex),
	))

	return &types.MsgCreateAnonymousPostResponse{Id: postID}, nil
}
