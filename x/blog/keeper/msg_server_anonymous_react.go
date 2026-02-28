package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

func (k msgServer) AnonymousReact(ctx context.Context, msg *types.MsgAnonymousReact) (*types.MsgAnonymousReactResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Submitter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid submitter address")
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

	// Validate reaction type
	if msg.ReactionType == types.ReactionType_REACTION_TYPE_UNSPECIFIED {
		return nil, errorsmod.Wrap(types.ErrInvalidReactionType, "reaction type must be specified")
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

	// Determine domain and scope
	var domain uint64
	var scope uint64
	if msg.ReplyId > 0 {
		// Reacting to a reply
		reply, replyFound := k.GetReply(ctx, msg.ReplyId)
		if !replyFound {
			return nil, errorsmod.Wrap(types.ErrReplyNotFound, fmt.Sprintf("reply %d not found", msg.ReplyId))
		}
		if reply.PostId != msg.PostId {
			return nil, errorsmod.Wrap(types.ErrReplyNotFound, "reply does not belong to this post")
		}
		if reply.Status == types.ReplyStatus_REPLY_STATUS_DELETED {
			return nil, errorsmod.Wrap(types.ErrReplyDeleted, fmt.Sprintf("reply %d has been deleted", msg.ReplyId))
		}
		if reply.Status == types.ReplyStatus_REPLY_STATUS_HIDDEN {
			return nil, errorsmod.Wrap(types.ErrReplyHidden, fmt.Sprintf("reply %d is hidden", msg.ReplyId))
		}
		domain = 9 // Anonymous reply reaction
		scope = msg.ReplyId
	} else {
		domain = 8 // Anonymous post reaction
		scope = msg.PostId
	}

	// Validate min_trust_level >= params.AnonymousMinTrustLevel
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

	// Check nullifier not already used
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	if k.IsNullifierUsed(ctx, domain, scope, nullifierHex) {
		return nil, errorsmod.Wrap(types.ErrNullifierUsed, "nullifier already used for this target")
	}

	// Verify ZK proof
	if err := k.voteKeeper.VerifyAnonymousActionProof(ctx, msg.Proof, msg.Nullifier, msg.MerkleRoot, msg.MinTrustLevel); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidProof, err.Error())
	}

	// Rate limit check (shared with regular reactions)
	if err := k.checkRateLimit(ctx, "reaction", submitterAddr, params.MaxReactionsPerDay); err != nil {
		return nil, err
	}

	// Charge reaction fee (with subsidy support for approved relays)
	if !params.ReactionFeeExempt && params.ReactionFee.IsPositive() {
		chargeToSubmitter := params.ReactionFee
		// Check if submitter is an approved relay eligible for subsidy
		if isApprovedRelay(msg.Submitter, params.AnonSubsidyRelayAddresses) &&
			!params.AnonSubsidyMaxPerPost.Amount.IsNil() && params.AnonSubsidyMaxPerPost.IsPositive() {
			// Subsidy logic — same as anonymous post/reply
			moduleAccAddr := sdk.AccAddress(authtypes.NewModuleAddress(types.ModuleName))
			subsidyBalance := k.bankKeeper.SpendableCoins(ctx, moduleAccAddr)
			maxSubsidy := params.AnonSubsidyMaxPerPost
			if chargeToSubmitter.IsLT(maxSubsidy) {
				maxSubsidy = chargeToSubmitter
			}
			availableSubsidy := subsidyBalance.AmountOf(maxSubsidy.Denom)
			if availableSubsidy.IsPositive() {
				subsidyAmt := maxSubsidy.Amount
				if availableSubsidy.LT(subsidyAmt) {
					subsidyAmt = availableSubsidy
				}
				subsidyCoin := sdk.NewCoin(maxSubsidy.Denom, subsidyAmt)
				if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(subsidyCoin)); err == nil {
					chargeToSubmitter = sdk.NewCoin(chargeToSubmitter.Denom, chargeToSubmitter.Amount.Sub(subsidyAmt))
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

	// Increment the appropriate reaction count
	counts := k.GetReactionCounts(ctx, msg.PostId, msg.ReplyId)
	adjustReactionCount(&counts, msg.ReactionType, 1)
	k.SetReactionCounts(ctx, msg.PostId, msg.ReplyId, counts)

	// Record nullifier
	k.SetNullifierUsed(ctx, domain, scope, nullifierHex, types.AnonNullifierEntry{
		UsedAt: sdkCtx.BlockTime().Unix(),
		Domain: domain,
		Scope:  scope,
	})

	// Increment rate limit
	k.incrementRateLimit(ctx, "reaction", submitterAddr)

	// Emit event (does NOT include submitter)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.anonymous_reaction.added",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.ReplyId)),
		sdk.NewAttribute("reaction_type", msg.ReactionType.String()),
		sdk.NewAttribute("proven_trust_level", fmt.Sprintf("%d", msg.MinTrustLevel)),
	))

	return &types.MsgAnonymousReactResponse{}, nil
}
