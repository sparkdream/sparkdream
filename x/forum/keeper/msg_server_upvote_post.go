package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UpvotePost(ctx context.Context, msg *types.MsgUpvotePost) (*types.MsgUpvotePostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Check reactions_enabled param
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if !params.ReactionsEnabled {
		return nil, types.ErrReactionsDisabled
	}

	// Load post
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	// Check post status - cannot vote on hidden/deleted/archived posts
	switch post.Status {
	case types.PostStatus_POST_STATUS_HIDDEN:
		return nil, types.ErrPostAlreadyHidden
	case types.PostStatus_POST_STATUS_DELETED:
		return nil, types.ErrPostDeleted
	case types.PostStatus_POST_STATUS_ARCHIVED:
		return nil, types.ErrPostArchived
	}

	// Cannot vote on your own post
	if post.Author == msg.Creator {
		return nil, types.ErrCannotVoteOwnPost
	}

	// Check if user already voted on this post (prevents duplicate voting)
	voteKey := collections.Join(msg.PostId, msg.Creator)
	hasVoted, err := k.PostVote.Has(ctx, voteKey)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check vote record")
	}
	if hasVoted {
		return nil, types.ErrAlreadyVoted
	}

	// Check and update reaction rate limit
	if err := k.checkAndUpdateReactionLimit(ctx, msg.Creator, now); err != nil {
		return nil, err
	}

	// Check membership for spam tax
	isMember := k.IsMember(ctx, msg.Creator)
	if !isMember {
		// Charge reaction_spam_tax to non-members
		if params.ReactionSpamTax.IsPositive() {
			creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
			if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, sdk.NewCoins(params.ReactionSpamTax)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to charge reaction spam tax")
			}
		}
	}

	// Increment upvote count
	post.UpvoteCount++

	// Store updated post
	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// Record individual vote to prevent duplicates
	if err := k.PostVote.Set(ctx, voteKey); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store vote record")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_upvoted",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("voter", msg.Creator),
			sdk.NewAttribute("is_member", fmt.Sprintf("%t", isMember)),
		),
	)

	return &types.MsgUpvotePostResponse{}, nil
}

// checkAndUpdateReactionLimit checks and updates the reaction rate limit for a user.
func (k msgServer) checkAndUpdateReactionLimit(ctx context.Context, addr string, now int64) error {
	reactionLimit, err := k.UserReactionLimit.Get(ctx, addr)
	if err != nil {
		// Create new reaction limit record
		reactionLimit = types.UserReactionLimit{
			UserAddress:      addr,
			CurrentDayCount:  0,
			PreviousDayCount: 0,
			CurrentDayStart:  now,
		}
	}

	// Day rotation (24h window)
	const dayDuration int64 = 86400
	if now-reactionLimit.CurrentDayStart >= dayDuration {
		reactionLimit.PreviousDayCount = reactionLimit.CurrentDayCount
		reactionLimit.CurrentDayCount = 0
		reactionLimit.CurrentDayStart = now
	}

	// Calculate effective count using sliding window approximation
	var overlapRatio float64
	elapsed := now - reactionLimit.CurrentDayStart
	if elapsed < dayDuration {
		overlapRatio = float64(dayDuration-elapsed) / float64(dayDuration)
	}
	effectiveCount := float64(reactionLimit.CurrentDayCount) + float64(reactionLimit.PreviousDayCount)*overlapRatio

	if effectiveCount >= float64(types.DefaultMaxReactionsPerDay) {
		return types.ErrReactionLimitExceeded
	}

	// Update reaction limit
	reactionLimit.CurrentDayCount++

	if err := k.UserReactionLimit.Set(ctx, addr, reactionLimit); err != nil {
		return errorsmod.Wrap(err, "failed to update reaction limit")
	}

	return nil
}
