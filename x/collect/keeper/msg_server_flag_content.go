package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) FlagContent(ctx context.Context, msg *types.MsgFlagContent) (*types.MsgFlagContentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Creator must be an active x/rep member
	if !k.isMember(ctx, msg.Creator) {
		return nil, types.ErrNotMember
	}

	// Target must be PUBLIC, ACTIVE (no community_feedback_enabled check for flags)
	if _, err := k.ValidatePublicActiveTarget(ctx, msg.TargetType, msg.TargetId); err != nil {
		return nil, err
	}

	// Get params
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Validate reason
	if msg.Reason == commontypes.ModerationReason_MODERATION_REASON_UNSPECIFIED {
		return nil, types.ErrInvalidFlagReason
	}

	// Validate reason_text: required for OTHER, forbidden for other reasons
	if msg.Reason == commontypes.ModerationReason_MODERATION_REASON_OTHER {
		if len(msg.ReasonText) == 0 {
			return nil, types.ErrFlagReasonTextRequired
		}
		if uint32(len(msg.ReasonText)) > params.MaxFlagReasonLength {
			return nil, types.ErrFlagReasonTextTooLong
		}
	} else if msg.ReasonText != "" {
		return nil, types.ErrInvalidFlagReason
	}

	// Build flag composite key
	flagKey := FlagCompositeKey(msg.TargetType, msg.TargetId)

	// Get or create CollectionFlag
	flag, err := k.Flag.Get(ctx, flagKey)
	if err != nil {
		// New flag record for this target
		flag = types.CollectionFlag{
			TargetId:      msg.TargetId,
			TargetType:    msg.TargetType,
			FlagRecords:   []commontypes.FlagRecord{},
			TotalWeight:   math.ZeroInt(),
			FirstFlagAt:   blockHeight,
			LastFlagAt:    blockHeight,
			InReviewQueue: false,
		}
	} else {
		// Check if creator already flagged this target
		for _, record := range flag.FlagRecords {
			if record.Flagger == msg.Creator {
				return nil, types.ErrAlreadyFlagged
			}
		}
	}

	// Check daily limit
	if err := k.checkDailyLimit(ctx, msg.Creator, blockHeight, "flag", params.MaxFlagsPerDay); err != nil {
		return nil, err
	}

	// Cap flag_records at max_flaggers_per_target
	if uint32(len(flag.FlagRecords)) >= params.MaxFlaggersPerTarget {
		return nil, types.ErrMaxFlagsPerTarget
	}

	// Create new FlagRecord with weight=2
	weight := math.NewInt(2)
	newRecord := commontypes.FlagRecord{
		Flagger:    msg.Creator,
		Reason:     msg.Reason,
		ReasonText: msg.ReasonText,
		FlaggedAt:  blockHeight,
		Weight:     weight,
	}

	flag.FlagRecords = append(flag.FlagRecords, newRecord)
	flag.TotalWeight = flag.TotalWeight.Add(weight)
	flag.LastFlagAt = blockHeight

	// If total_weight >= flag_review_threshold and not already in_review_queue: set in_review_queue=true
	if flag.TotalWeight.GTE(math.NewIntFromUint64(uint64(params.FlagReviewThreshold))) && !flag.InReviewQueue {
		flag.InReviewQueue = true
		if err := k.FlagReviewQueue.Set(ctx, collections.Join(int32(msg.TargetType), msg.TargetId)); err != nil {
			return nil, errorsmod.Wrap(err, "failed to set flag review queue")
		}
	}

	// Store flag
	if err := k.Flag.Set(ctx, flagKey, flag); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store flag")
	}

	// Update FlagExpiry index: remove old entry (if any), add new one
	// Use lastFlagAt + expiration for the expiry key
	// Old entries with different expiry will be cleaned by the EndBlocker
	expiryBlock := blockHeight + params.FlagExpirationBlocks
	if err := k.FlagExpiry.Set(ctx, collections.Join(expiryBlock, flagKey)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set flag expiry")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("content_flagged",
		sdk.NewAttribute("creator", msg.Creator),
		sdk.NewAttribute("target_id", strconv.FormatUint(msg.TargetId, 10)),
		sdk.NewAttribute("target_type", msg.TargetType.String()),
		sdk.NewAttribute("reason", msg.Reason.String()),
		sdk.NewAttribute("total_weight", flag.TotalWeight.String()),
		sdk.NewAttribute("in_review_queue", strconv.FormatBool(flag.InReviewQueue)),
	))

	return &types.MsgFlagContentResponse{}, nil
}
