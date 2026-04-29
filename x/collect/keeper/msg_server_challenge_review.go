package keeper

import (
	"context"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
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

	// Reserve slash budget against the curator's bonded role. Amount equals
	// CuratorSlashFraction × MinCuratorBond — a fixed per-challenge commit so
	// resolution slashes only this chunk (not a fraction of the whole bond).
	// This is the Phase-3 commit-per-action upgrade: the curator can hold
	// multiple challenges concurrently as long as their available bond covers
	// the sum of reservations.
	slashBudget := params.CuratorSlashFraction.MulInt(params.MinCuratorBond).TruncateInt()

	// Reject challenges when the curator is already demoted and their bond
	// cannot cover the commit — the reservation would fail anyway, but
	// rejecting early avoids locking the challenger's deposit first.
	if curatorRole, rErr := k.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR, review.Curator); rErr == nil {
		curatorBond, ok := math.NewIntFromString(curatorRole.CurrentBond)
		if !ok {
			curatorBond = math.ZeroInt()
		}
		if curatorRole.BondStatus == reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED && curatorBond.LT(slashBudget) {
			return nil, errorsmod.Wrap(types.ErrCuratorBondInsufficient, "curator demoted with bond below slash budget")
		}
	}

	if slashBudget.IsPositive() {
		if err := k.repKeeper.ReserveBond(ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR, review.Curator, slashBudget); err != nil {
			// Refund the already-locked challenge deposit so the challenger
			// isn't stuck paying for a failed reservation.
			_ = k.repKeeper.UnlockDREAM(ctx, creatorAddr, params.ChallengeDeposit)
			return nil, errorsmod.Wrap(err, "failed to reserve curator slash budget")
		}
	}

	// Mark review challenged=true, set challenger + committed_slash.
	review.Challenged = true
	review.Challenger = msg.Creator
	review.CommittedSlash = slashBudget
	if err := k.CurationReview.Set(ctx, msg.ReviewId, review); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update review")
	}

	// Bump per-module curator activity counters.
	activity, _ := k.CuratorActivity.Get(ctx, review.Curator)
	if activity.Address == "" {
		activity.Address = review.Curator
	}
	activity.ChallengedReviews++
	if err := k.CuratorActivity.Set(ctx, review.Curator, activity); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update curator activity")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("review_challenged",
		sdk.NewAttribute("review_id", strconv.FormatUint(msg.ReviewId, 10)),
		sdk.NewAttribute("collection_id", strconv.FormatUint(review.CollectionId, 10)),
		sdk.NewAttribute("challenger", msg.Creator),
		sdk.NewAttribute("curator", review.Curator),
		sdk.NewAttribute("challenge_deposit", params.ChallengeDeposit.String()),
		sdk.NewAttribute("committed_slash", slashBudget.String()),
	))

	return &types.MsgChallengeReviewResponse{}, nil
}
