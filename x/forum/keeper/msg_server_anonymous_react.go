package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

	// Check reactions enabled AND private reactions enabled
	if !params.ReactionsEnabled {
		return nil, types.ErrReactionsDisabled
	}
	if !params.PrivateReactionsEnabled {
		return nil, errorsmod.Wrap(types.ErrPrivateReactionsDisabled, "private reactions are not enabled")
	}

	// Check VoteKeeper available
	if k.voteKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrAnonPostingDisabled, "vote module not available")
	}

	// Get post, must exist and be active
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}
	switch post.Status {
	case types.PostStatus_POST_STATUS_HIDDEN:
		return nil, types.ErrPostAlreadyHidden
	case types.PostStatus_POST_STATUS_DELETED:
		return nil, types.ErrPostDeleted
	case types.PostStatus_POST_STATUS_ARCHIVED:
		return nil, types.ErrPostArchived
	}

	// Validate reaction_type: 1 = UPVOTE, 2 = DOWNVOTE
	if msg.ReactionType < 1 || msg.ReactionType > 2 {
		return nil, errorsmod.Wrap(types.ErrInvalidReactionType, "reaction_type must be 1 (upvote) or 2 (downvote)")
	}

	// If downvote, burn downvote_deposit from submitter (with optional subsidy)
	if msg.ReactionType == 2 && params.DownvoteDeposit.IsPositive() {
		// Compute epoch for subsidy tracking
		epochDuration := DefaultEpochDuration
		if k.seasonKeeper != nil {
			epochDuration = k.seasonKeeper.GetEpochDuration(ctx)
		}
		epoch := uint64(sdkCtx.BlockTime().Unix()) / uint64(epochDuration)

		subsidy := k.TrySubsidizeAnonymousAction(ctx, params, msg.Submitter, params.DownvoteDeposit, epoch)
		netFee := params.DownvoteDeposit.Sub(subsidy)
		if netFee.IsPositive() {
			if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, submitterAddr, types.ModuleName, sdk.NewCoins(netFee)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to charge downvote deposit")
			}
			if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(netFee)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to burn downvote deposit")
			}
		}
	}

	// Validate min_trust_level
	if msg.MinTrustLevel < params.AnonymousMinTrustLevel {
		return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
			"min_trust_level %d below required %d", msg.MinTrustLevel, params.AnonymousMinTrustLevel)
	}

	// Validate merkle root
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

	// Domain=5 for forum reactions, Scope=postId (one anonymous reaction per member per post)
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	if k.IsNullifierUsed(ctx, 5, msg.PostId, nullifierHex) {
		return nil, errorsmod.Wrap(types.ErrNullifierUsed, "nullifier already used for this post")
	}

	// Verify ZK proof
	if err := k.voteKeeper.VerifyAnonymousActionProof(ctx, msg.Proof, msg.Nullifier, msg.MerkleRoot, msg.MinTrustLevel); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidProof, err.Error())
	}

	// Note: the nullifier (domain=5, one per post per member) provides stronger
	// spam protection than the reaction rate limit. Applying the rate limit to the
	// relay address would leak relay activity patterns without adding security.

	// Increment vote count on the post
	if msg.ReactionType == 1 {
		post.UpvoteCount++
	} else {
		post.DownvoteCount++
	}
	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post vote counts")
	}

	// Record nullifier (domain=5, scope=postId)
	now := sdkCtx.BlockTime().Unix()
	k.SetNullifierUsed(ctx, 5, msg.PostId, nullifierHex, types.AnonNullifierEntry{
		UsedAt: now,
		Domain: 5,
		Scope:  msg.PostId,
	})

	// Emit event
	reactionStr := "upvote"
	if msg.ReactionType == 2 {
		reactionStr = "downvote"
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("forum.anonymous_react",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
		sdk.NewAttribute("reaction_type", reactionStr),
		sdk.NewAttribute("proven_trust_level", fmt.Sprintf("%d", msg.MinTrustLevel)),
		sdk.NewAttribute("nullifier_hex", nullifierHex),
	))

	return &types.MsgAnonymousReactResponse{}, nil
}
