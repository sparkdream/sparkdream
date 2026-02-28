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

func (k msgServer) CreateAnonymousPost(ctx context.Context, msg *types.MsgCreateAnonymousPost) (*types.MsgCreateAnonymousPostResponse, error) {
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

	// Validate title length
	if len(msg.Title) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "title cannot be empty")
	}
	if uint64(len(msg.Title)) > params.MaxTitleLength {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "title too long: %d > %d", len(msg.Title), params.MaxTitleLength)
	}

	// Validate body length
	if len(msg.Body) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "body cannot be empty")
	}
	if uint64(len(msg.Body)) > params.MaxBodyLength {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "body too long: %d > %d", len(msg.Body), params.MaxBodyLength)
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

	// Check nullifier not used (domain=1 for posts)
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	if k.IsNullifierUsed(ctx, 1, epoch, nullifierHex) {
		return nil, errorsmod.Wrap(types.ErrNullifierUsed, "nullifier already used in this epoch")
	}

	// Verify ZK proof
	if err := k.voteKeeper.VerifyAnonymousActionProof(ctx, msg.Proof, msg.Nullifier, msg.MerkleRoot, msg.MinTrustLevel); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidProof, err.Error())
	}

	// Rate limit check (shared with regular posts)
	if err := k.checkRateLimit(ctx, "post", submitterAddr, params.MaxPostsPerDay); err != nil {
		return nil, err
	}

	// Charge storage fee (with subsidy support for approved relays)
	if !params.CostPerByteExempt && params.CostPerByte.IsPositive() {
		contentBytes := int64(len(msg.Title)) + int64(len(msg.Body))
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

	// Create post with module account as creator
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	post := types.Post{
		Creator:            moduleAddr,
		Title:              msg.Title,
		Body:               msg.Body,
		ContentType:        msg.ContentType,
		RepliesEnabled:     true,
		MinReplyTrustLevel: 0,
		CreatedAt:          sdkCtx.BlockTime().Unix(),
		Status:             types.PostStatus_POST_STATUS_ACTIVE,
		ExpiresAt:          expiresAt,
		FeeBytesHighWater:  uint64(len(msg.Title) + len(msg.Body)),
	}
	id := k.AppendPost(ctx, post)

	// Add to expiry index if ephemeral
	if expiresAt > 0 {
		k.AddToExpiryIndex(ctx, expiresAt, "post", id)
	}

	// Store anonymous metadata
	k.SetAnonymousPostMeta(ctx, id, types.AnonymousPostMetadata{
		ContentId:        id,
		Nullifier:        msg.Nullifier,
		MerkleRoot:       msg.MerkleRoot,
		ProvenTrustLevel: msg.MinTrustLevel,
	})

	// Record nullifier
	k.SetNullifierUsed(ctx, 1, epoch, nullifierHex, types.AnonNullifierEntry{
		UsedAt: sdkCtx.BlockTime().Unix(),
		Domain: 1,
		Scope:  epoch,
	})

	// Increment rate limit
	k.incrementRateLimit(ctx, "post", submitterAddr)

	// Emit event (does NOT include submitter)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.anonymous_post.created",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("proven_trust_level", fmt.Sprintf("%d", msg.MinTrustLevel)),
		sdk.NewAttribute("nullifier_hex", nullifierHex),
		sdk.NewAttribute("expires_at", fmt.Sprintf("%d", expiresAt)),
	))

	return &types.MsgCreateAnonymousPostResponse{Id: id}, nil
}

// isApprovedRelay checks if an address is in the list of approved relay addresses.
func isApprovedRelay(addr string, relays []string) bool {
	for _, r := range relays {
		if r == addr {
			return true
		}
	}
	return false
}
