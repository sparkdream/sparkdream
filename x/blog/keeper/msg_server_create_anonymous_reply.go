package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

func (k msgServer) CreateAnonymousReply(ctx context.Context, msg *types.MsgCreateAnonymousReply) (*types.MsgCreateAnonymousReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Submitter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	submitterAddr, _ := sdk.AccAddressFromBech32(msg.Submitter)
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Check anonymous posting enabled
	if !params.AnonymousPostingEnabled {
		return nil, errorsmod.Wrap(types.ErrAnonPostingDisabled, "anonymous posting is not enabled")
	}

	// Check VoteKeeper available
	if k.voteKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrAnonPostingDisabled, "vote module not available")
	}

	// Get post, must exist and be active
	post, found := k.GetPost(ctx, msg.PostId)
	if !found {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}
	if post.Status == types.PostStatus_POST_STATUS_DELETED {
		return nil, errorsmod.Wrap(types.ErrPostDeleted, fmt.Sprintf("post %d has been deleted", msg.PostId))
	}
	if post.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrPostHidden, fmt.Sprintf("post %d is hidden", msg.PostId))
	}

	// Replies must be enabled
	if !post.RepliesEnabled {
		return nil, errorsmod.Wrap(types.ErrRepliesDisabled, "replies are disabled for this post")
	}

	// Validate body length
	if len(msg.Body) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "body cannot be empty")
	}
	if uint64(len(msg.Body)) > params.MaxReplyLength {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "body too long: %d > %d", len(msg.Body), params.MaxReplyLength)
	}

	// Validate min_trust_level >= max(params.AnonymousMinTrustLevel, post.MinReplyTrustLevel)
	requiredLevel := params.AnonymousMinTrustLevel
	if post.MinReplyTrustLevel > 0 && uint32(post.MinReplyTrustLevel) > requiredLevel {
		requiredLevel = uint32(post.MinReplyTrustLevel)
	}
	if msg.MinTrustLevel < requiredLevel {
		return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
			"min_trust_level %d below required %d", msg.MinTrustLevel, requiredLevel)
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

	// Domain=2 for replies, Scope=postId
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	if k.IsNullifierUsed(ctx, 2, msg.PostId, nullifierHex) {
		return nil, errorsmod.Wrap(types.ErrNullifierUsed, "nullifier already used for this post")
	}

	// Verify ZK proof
	if err := k.voteKeeper.VerifyAnonymousActionProof(ctx, msg.Proof, msg.Nullifier, msg.MerkleRoot, msg.MinTrustLevel); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidProof, err.Error())
	}

	// Rate limit check
	if err := k.checkRateLimit(ctx, "reply", submitterAddr, params.MaxRepliesPerDay); err != nil {
		return nil, err
	}

	// Charge storage fee (with subsidy support for approved relays)
	if !params.CostPerByteExempt && params.CostPerByte.IsPositive() {
		contentBytes := int64(len(msg.Body))
		storageFee := sdk.NewCoin(params.CostPerByte.Denom, params.CostPerByte.Amount.MulRaw(contentBytes))
		if storageFee.IsPositive() {
			chargeToSubmitter := storageFee
			// Check if submitter is an approved relay eligible for subsidy
			if isApprovedRelay(msg.Submitter, params.AnonSubsidyRelayAddresses) &&
				!params.AnonSubsidyMaxPerPost.Amount.IsNil() && params.AnonSubsidyMaxPerPost.IsPositive() {
				moduleAccAddr := authtypes.NewModuleAddress(types.ModuleName)
				subsidyBalance := k.bankKeeper.SpendableCoins(ctx, moduleAccAddr)
				maxSubsidy := params.AnonSubsidyMaxPerPost
				if storageFee.IsLT(maxSubsidy) {
					maxSubsidy = storageFee
				}
				availableSubsidy := subsidyBalance.AmountOf(maxSubsidy.Denom)
				if availableSubsidy.IsPositive() {
					subsidyAmt := maxSubsidy.Amount
					if availableSubsidy.LT(subsidyAmt) {
						subsidyAmt = availableSubsidy
					}
					subsidyCoin := sdk.NewCoin(maxSubsidy.Denom, subsidyAmt)
					if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(subsidyCoin)); err == nil {
						chargeToSubmitter = sdk.NewCoin(storageFee.Denom, storageFee.Amount.Sub(subsidyAmt))
					}
				}
			}
			if chargeToSubmitter.IsPositive() {
				if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, submitterAddr, types.ModuleName, sdk.NewCoins(chargeToSubmitter)); err != nil {
					return nil, err
				}
				if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(chargeToSubmitter)); err != nil {
					return nil, err
				}
			}
		}
	}

	// Compute ephemeral TTL
	var expiresAt int64
	if params.EphemeralContentTtl > 0 {
		expiresAt = sdkCtx.BlockTime().Unix() + params.EphemeralContentTtl
	}

	// Create reply with module account as creator, always top-level
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	reply := types.Reply{
		PostId:            msg.PostId,
		ParentReplyId:     0,
		Creator:           moduleAddr,
		Body:              msg.Body,
		ContentType:       msg.ContentType,
		CreatedAt:         sdkCtx.BlockTime().Unix(),
		Depth:             0,
		Status:            types.ReplyStatus_REPLY_STATUS_ACTIVE,
		ExpiresAt:         expiresAt,
		FeeBytesHighWater: uint64(len(msg.Body)),
	}
	id := k.AppendReply(ctx, reply)

	// Add to expiry index if ephemeral
	if expiresAt > 0 {
		k.AddToExpiryIndex(ctx, expiresAt, "reply", id)
	}

	// Increment post reply count
	post.ReplyCount++
	k.SetPost(ctx, post)

	// Store anonymous metadata
	k.SetAnonymousReplyMeta(ctx, id, types.AnonymousPostMetadata{
		ContentId:        id,
		Nullifier:        msg.Nullifier,
		MerkleRoot:       msg.MerkleRoot,
		ProvenTrustLevel: msg.MinTrustLevel,
	})

	// Record nullifier (domain=2, scope=postId)
	k.SetNullifierUsed(ctx, 2, msg.PostId, nullifierHex, types.AnonNullifierEntry{
		UsedAt: sdkCtx.BlockTime().Unix(),
		Domain: 2,
		Scope:  msg.PostId,
	})

	// Increment rate limit
	k.incrementRateLimit(ctx, "reply", submitterAddr)

	// Emit event (does NOT include submitter)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.anonymous_reply.created",
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
		sdk.NewAttribute("proven_trust_level", fmt.Sprintf("%d", msg.MinTrustLevel)),
		sdk.NewAttribute("nullifier_hex", nullifierHex),
		sdk.NewAttribute("expires_at", fmt.Sprintf("%d", expiresAt)),
	))

	return &types.MsgCreateAnonymousReplyResponse{Id: id}, nil
}
