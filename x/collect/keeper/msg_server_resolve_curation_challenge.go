package keeper

import (
	"context"
	"strconv"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ResolveCurationChallenge delivers the verdict on a pending curation
// challenge. Curation challenges have no x/rep jury of their own (the shared
// moderation-appeal machinery slashes the content-sentinel role, not the
// curator role), so the verdict is a committee decision: only the gov
// authority, the Commons Council policy or the Commons Operations Committee
// policy (an executed proposal) may sign it, never an individual committee
// member, because it moves a curator's bond and a challenger's deposit.
//
// The CurationChallengeDeadline entry is left in place: the timeout sweep
// drops it as stale once the challenge is settled (isChallengePending false).
func (k msgServer) ResolveCurationChallenge(ctx context.Context, msg *types.MsgResolveCurationChallenge) (*types.MsgResolveCurationChallengeResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	if k.commonsKeeper == nil || !k.commonsKeeper.IsCouncilOrCommitteePolicy(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrUnauthorized,
			"only the gov authority, the Commons Council or the Commons Operations Committee policy may resolve curation challenges")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}
	if commontypes.Saturate[uint32](len(msg.Reason)) > params.MaxChallengeReasonLength {
		return nil, errorsmod.Wrapf(types.ErrReasonTooLong, "reason length %d exceeds max %d",
			len(msg.Reason), params.MaxChallengeReasonLength)
	}

	review, err := k.CurationReview.Get(ctx, msg.ReviewId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrReviewNotFound, "review not found")
	}
	if !isChallengePending(review) {
		return nil, errorsmod.Wrap(types.ErrReviewNotFound, "review has no pending challenge")
	}

	if err := k.ResolveChallengeResult(ctx, msg.ReviewId, msg.Upheld); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("curation_challenge_verdict",
		sdk.NewAttribute("review_id", strconv.FormatUint(msg.ReviewId, 10)),
		sdk.NewAttribute("upheld", strconv.FormatBool(msg.Upheld)),
		sdk.NewAttribute("resolver", msg.Creator),
		sdk.NewAttribute("reason", msg.Reason),
	))

	return &types.MsgResolveCurationChallengeResponse{}, nil
}
