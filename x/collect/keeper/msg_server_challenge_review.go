package keeper

import (
	"context"
	"strconv"

	"sparkdream/x/collect/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) ChallengeReview(ctx context.Context, msg *types.MsgChallengeReview) (*types.MsgChallengeReviewResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Creator must be active x/rep member
	if !k.isMember(ctx, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotMember, msg.Creator)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Review must exist
	review, err := k.CurationReview.Get(ctx, msg.ReviewId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrReviewNotFound, "review not found")
	}

	// Not already challenged or overturned
	if review.Challenged {
		return nil, errorsmod.Wrap(types.ErrReviewAlreadyChallenged, "review is already challenged")
	}
	if review.Overturned {
		return nil, errorsmod.Wrap(types.ErrReviewAlreadyChallenged, "review is already overturned")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	// Review's parent collection not expired
	coll, err := k.Collection.Get(ctx, review.CollectionId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, "parent collection not found")
	}
	if coll.ExpiresAt > 0 && coll.ExpiresAt <= currentBlock {
		return nil, errorsmod.Wrap(types.ErrCollectionExpired, "parent collection has expired")
	}

	// Within challenge_window_blocks of review creation
	if currentBlock-review.CreatedAt > params.ChallengeWindowBlocks {
		return nil, errorsmod.Wrapf(types.ErrChallengeWindowExpired, "review created at %d, window is %d blocks",
			review.CreatedAt, params.ChallengeWindowBlocks)
	}

	// Creator != review's curator
	if msg.Creator == review.Curator {
		return nil, errorsmod.Wrap(types.ErrCannotChallengeSelf, "cannot challenge own review")
	}

	// reason <= max_challenge_reason_length
	if uint32(len(msg.Reason)) > params.MaxChallengeReasonLength {
		return nil, errorsmod.Wrapf(types.ErrReasonTooLong, "reason length %d exceeds max %d",
			len(msg.Reason), params.MaxChallengeReasonLength)
	}

	// Lock challenge_deposit DREAM from creator via repKeeper.LockDREAM
	if err := k.repKeeper.LockDREAM(ctx, creatorAddr, params.ChallengeDeposit); err != nil {
		return nil, errorsmod.Wrap(err, "failed to lock challenge deposit")
	}

	// Mark review challenged=true, set challenger
	review.Challenged = true
	review.Challenger = msg.Creator
	if err := k.CurationReview.Set(ctx, msg.ReviewId, review); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update review")
	}

	// Increment pending_challenges on curator
	curator, err := k.Curator.Get(ctx, review.Curator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get curator")
	}
	curator.PendingChallenges++
	if err := k.Curator.Set(ctx, review.Curator, curator); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update curator")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("review_challenged",
		sdk.NewAttribute("review_id", strconv.FormatUint(msg.ReviewId, 10)),
		sdk.NewAttribute("collection_id", strconv.FormatUint(review.CollectionId, 10)),
		sdk.NewAttribute("challenger", msg.Creator),
		sdk.NewAttribute("curator", review.Curator),
		sdk.NewAttribute("challenge_deposit", params.ChallengeDeposit.String()),
	))

	return &types.MsgChallengeReviewResponse{}, nil
}
