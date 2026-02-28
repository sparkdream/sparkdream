package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

func (k msgServer) CreateAnonymousReply(ctx context.Context, msg *types.MsgCreateAnonymousReply) (*types.MsgCreateAnonymousReplyResponse, error) {
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

	// Get parent post, must exist and be active
	parentPost, err := k.Post.Get(ctx, msg.ParentId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("parent post %d not found", msg.ParentId))
	}
	switch parentPost.Status {
	case types.PostStatus_POST_STATUS_HIDDEN:
		return nil, errorsmod.Wrap(types.ErrPostAlreadyHidden, fmt.Sprintf("parent post %d is hidden", msg.ParentId))
	case types.PostStatus_POST_STATUS_DELETED:
		return nil, errorsmod.Wrap(types.ErrPostDeleted, fmt.Sprintf("parent post %d has been deleted", msg.ParentId))
	case types.PostStatus_POST_STATUS_ARCHIVED:
		return nil, errorsmod.Wrap(types.ErrPostArchived, fmt.Sprintf("parent post %d is archived", msg.ParentId))
	}

	// Determine root ID
	var rootID uint64
	if parentPost.ParentId == 0 {
		rootID = msg.ParentId
	} else {
		rootID = parentPost.RootId
	}

	// Check if thread is locked
	rootPost, err := k.Post.Get(ctx, rootID)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("root post %d not found", rootID))
	}
	if rootPost.Locked {
		return nil, types.ErrThreadLocked
	}

	// Check category allows anonymous
	category, err := k.Category.Get(ctx, rootPost.CategoryId)
	if err == nil && !category.AllowAnonymous {
		return nil, errorsmod.Wrap(types.ErrCategoryAnonNotAllowed, "category does not allow anonymous posts")
	}

	// Check reply depth
	depth := parentPost.Depth + 1
	if depth > uint64(params.MaxReplyDepth) {
		return nil, errorsmod.Wrapf(types.ErrMaxReplyDepthExceeded, "max depth is %d", params.MaxReplyDepth)
	}

	// Validate content
	if msg.Content == "" {
		return nil, types.ErrEmptyContent
	}
	if uint64(len(msg.Content)) > params.MaxContentSize {
		return nil, errorsmod.Wrapf(types.ErrContentTooLarge, "content too long: %d > %d", len(msg.Content), params.MaxContentSize)
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

	// Domain=4 for forum replies, Scope=rootID (one anonymous reply per member per thread)
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	if k.IsNullifierUsed(ctx, 4, rootID, nullifierHex) {
		return nil, errorsmod.Wrap(types.ErrNullifierUsed, "nullifier already used for this thread")
	}

	// Verify ZK proof
	if err := k.voteKeeper.VerifyAnonymousActionProof(ctx, msg.Proof, msg.Nullifier, msg.MerkleRoot, msg.MinTrustLevel); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidProof, err.Error())
	}

	now := sdkCtx.BlockTime().Unix()

	// Note: rate limiting is NOT applied to the relay address for anonymous replies.
	// The nullifier already prevents double-posting per thread, and rate-limiting
	// the relay would create a privacy-leaking correlation vector.

	// Charge storage fee (with optional subsidy for approved relays)
	if !params.CostPerByteExempt && params.CostPerByte.IsPositive() {
		contentBytes := int64(len(msg.Content))
		storageFee := sdk.NewCoin(params.CostPerByte.Denom, params.CostPerByte.Amount.MulRaw(contentBytes))
		if storageFee.IsPositive() {
			// Compute epoch for subsidy tracking
			epochDuration := DefaultEpochDuration
			if k.seasonKeeper != nil {
				epochDuration = k.seasonKeeper.GetEpochDuration(ctx)
			}
			epoch := uint64(sdkCtx.BlockTime().Unix()) / uint64(epochDuration)

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

	// Generate post ID
	postID, err := k.PostSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate post ID")
	}

	// Create reply with module account as author (permanent, no expiry)
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	post := types.Post{
		PostId:      postID,
		CategoryId:  rootPost.CategoryId,
		RootId:      rootID,
		ParentId:    msg.ParentId,
		Author:      moduleAddr,
		Content:     msg.Content,
		CreatedAt:   now,
		Status:      types.PostStatus_POST_STATUS_ACTIVE,
		Depth:       depth,
		ContentType: msg.ContentType,
	}

	if err := k.Post.Set(ctx, postID, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store reply")
	}

	// Store anonymous metadata
	k.SetAnonymousReplyMeta(ctx, postID, types.AnonymousPostMetadata{
		ContentId:        postID,
		Nullifier:        msg.Nullifier,
		MerkleRoot:       msg.MerkleRoot,
		ProvenTrustLevel: msg.MinTrustLevel,
	})

	// Record nullifier (domain=4, scope=rootID)
	k.SetNullifierUsed(ctx, 4, rootID, nullifierHex, types.AnonNullifierEntry{
		UsedAt: now,
		Domain: 4,
		Scope:  rootID,
	})

	// Emit standard post_created event for indexer compatibility (author = module account)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("post_created",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
		sdk.NewAttribute("category_id", fmt.Sprintf("%d", rootPost.CategoryId)),
		sdk.NewAttribute("parent_id", fmt.Sprintf("%d", msg.ParentId)),
		sdk.NewAttribute("author", moduleAddr),
		sdk.NewAttribute("is_ephemeral", "false"),
		sdk.NewAttribute("is_anonymous", "true"),
	))

	// Emit anonymous-specific event (no submitter — preserve relay anonymity)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("forum.anonymous_reply.created",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
		sdk.NewAttribute("parent_id", fmt.Sprintf("%d", msg.ParentId)),
		sdk.NewAttribute("root_id", fmt.Sprintf("%d", rootID)),
		sdk.NewAttribute("proven_trust_level", fmt.Sprintf("%d", msg.MinTrustLevel)),
		sdk.NewAttribute("nullifier_hex", nullifierHex),
	))

	return &types.MsgCreateAnonymousReplyResponse{Id: postID}, nil
}
