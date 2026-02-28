package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	commontypes "sparkdream/x/common/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) FlagPost(ctx context.Context, msg *types.MsgFlagPost) (*types.MsgFlagPostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Load post
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	// Check post status - cannot flag hidden/deleted/archived posts
	switch post.Status {
	case types.PostStatus_POST_STATUS_HIDDEN:
		return nil, types.ErrPostAlreadyHidden
	case types.PostStatus_POST_STATUS_DELETED:
		return nil, types.ErrPostDeleted
	case types.PostStatus_POST_STATUS_ARCHIVED:
		return nil, types.ErrPostArchived
	}

	// Check flag rate limit
	if err := k.checkAndUpdateFlagLimit(ctx, msg.Creator, now); err != nil {
		return nil, err
	}

	// Load or create flag record
	postFlag, err := k.PostFlag.Get(ctx, msg.PostId)
	if err != nil {
		postFlag = types.PostFlag{
			PostId:        msg.PostId,
			Flaggers:      []string{},
			TotalWeight:   "0",
			FirstFlagAt:   now,
			LastFlagAt:    now,
			InReviewQueue: false,
			FlagRecords:   []*commontypes.FlagRecord{},
			ReasonCounts:  make(map[int32]uint64),
		}
	}

	// Check if user already flagged this post
	for _, flagger := range postFlag.Flaggers {
		if flagger == msg.Creator {
			return nil, types.ErrAlreadyFlagged
		}
	}

	// Check max flaggers
	if uint64(len(postFlag.Flaggers)) >= types.DefaultMaxPostFlaggers {
		return nil, types.ErrMaxFlaggersReached
	}

	// Get params
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}

	// Determine flag weight based on membership
	isMember := k.IsMember(ctx, msg.Creator)
	var weight uint64
	if isMember {
		weight = types.DefaultMemberFlagWeight
	} else {
		weight = types.DefaultNonmemberFlagWeight
		// Charge flag_spam_tax to non-members
		if params.FlagSpamTax.IsPositive() {
			creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
			if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, sdk.NewCoins(params.FlagSpamTax)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to charge flag spam tax")
			}
		}
	}

	// Add flagger
	postFlag.Flaggers = append(postFlag.Flaggers, msg.Creator)
	postFlag.LastFlagAt = now

	// Create flag record with reason
	reasonCode := commontypes.ModerationReason(msg.Category)
	flagRecord := &commontypes.FlagRecord{
		Flagger:    msg.Creator,
		Reason:     reasonCode,
		ReasonText: msg.Reason,
		FlaggedAt:  now,
		Weight:     math.NewInt(int64(weight)),
	}
	postFlag.FlagRecords = append(postFlag.FlagRecords, flagRecord)

	// Update reason counts
	if postFlag.ReasonCounts == nil {
		postFlag.ReasonCounts = make(map[int32]uint64)
	}
	postFlag.ReasonCounts[int32(reasonCode)]++

	// Update total weight
	currentWeight := uint64(0)
	if postFlag.TotalWeight != "" && postFlag.TotalWeight != "0" {
		fmt.Sscanf(postFlag.TotalWeight, "%d", &currentWeight)
	}
	currentWeight += weight
	postFlag.TotalWeight = fmt.Sprintf("%d", currentWeight)

	// Check if post should enter review queue
	if currentWeight >= types.DefaultFlagReviewThreshold && !postFlag.InReviewQueue {
		postFlag.InReviewQueue = true
	}

	// Store flag record
	if err := k.PostFlag.Set(ctx, msg.PostId, postFlag); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store flag record")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_flagged",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("flagger", msg.Creator),
			sdk.NewAttribute("reason_code", fmt.Sprintf("%d", msg.Category)),
			sdk.NewAttribute("weight", fmt.Sprintf("%d", weight)),
			sdk.NewAttribute("in_review_queue", fmt.Sprintf("%t", postFlag.InReviewQueue)),
		),
	)

	return &types.MsgFlagPostResponse{}, nil
}

// checkAndUpdateFlagLimit checks and updates the flag rate limit for a user.
func (k msgServer) checkAndUpdateFlagLimit(ctx context.Context, addr string, now int64) error {
	// Use a separate key for flag limit (prefix with "flag_")
	limitKey := "flag_" + addr

	reactionLimit, err := k.UserReactionLimit.Get(ctx, limitKey)
	if err != nil {
		// Create new flag limit record
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

	if effectiveCount >= float64(types.DefaultMaxFlagsPerDay) {
		return types.ErrFlagLimitExceeded
	}

	// Update flag limit
	reactionLimit.CurrentDayCount++

	if err := k.UserReactionLimit.Set(ctx, limitKey, reactionLimit); err != nil {
		return errorsmod.Wrap(err, "failed to update flag limit")
	}

	return nil
}
