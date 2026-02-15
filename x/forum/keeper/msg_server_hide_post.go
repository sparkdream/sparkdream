package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) HidePost(ctx context.Context, msg *types.MsgHidePost) (*types.MsgHidePostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Check moderation_paused param
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if params.ModerationPaused {
		return nil, types.ErrModerationPaused
	}

	// Validate reason code
	reasonCode := types.ModerationReason(msg.ReasonCode)
	if reasonCode == types.ModerationReason_MODERATION_REASON_UNSPECIFIED {
		return nil, types.ErrInvalidReasonCode
	}
	if reasonCode == types.ModerationReason_MODERATION_REASON_OTHER && msg.ReasonText == "" {
		return nil, types.ErrReasonTextRequired
	}

	// Check if sender is operations committee or sentinel
	isGovAuthority := k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	// Load sentinel activity for non-gov senders
	var sentinelActivity types.SentinelActivity
	if !isGovAuthority {
		var err error
		sentinelActivity, err = k.SentinelActivity.Get(ctx, msg.Creator)
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrNotSentinel, "not a registered sentinel")
		}

		// Check bond status
		if sentinelActivity.BondStatus == types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED {
			return nil, types.ErrSentinelDemoted
		}

		// Check cooldown
		if sentinelActivity.OverturnCooldownUntil > now {
			return nil, errorsmod.Wrapf(types.ErrSentinelCooldown,
				"cooldown until %d", sentinelActivity.OverturnCooldownUntil)
		}

		// Check hide limit
		if sentinelActivity.EpochHides >= types.DefaultMaxHidesPerEpoch {
			return nil, types.ErrHideLimitExceeded
		}

		// Check available bond for commitment
		currentBond, _ := math.NewIntFromString(sentinelActivity.CurrentBond)
		if sentinelActivity.CurrentBond == "" {
			currentBond = math.ZeroInt()
		}
		committedBond, _ := math.NewIntFromString(sentinelActivity.TotalCommittedBond)
		if sentinelActivity.TotalCommittedBond == "" {
			committedBond = math.ZeroInt()
		}
		availableBond := currentBond.Sub(committedBond)

		slashAmount := math.NewInt(100) // DefaultSentinelSlashAmount
		if availableBond.LT(slashAmount) {
			return nil, errorsmod.Wrapf(types.ErrInsufficientBond,
				"need %s DREAM available, only %s available", slashAmount.String(), availableBond.String())
		}
	}

	// Load post
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	// Check post status - cannot hide already hidden/deleted/archived posts
	switch post.Status {
	case types.PostStatus_POST_STATUS_HIDDEN:
		return nil, types.ErrPostAlreadyHidden
	case types.PostStatus_POST_STATUS_DELETED:
		return nil, types.ErrPostDeleted
	case types.PostStatus_POST_STATUS_ARCHIVED:
		return nil, types.ErrPostArchived
	}

	// Update post status
	post.Status = types.PostStatus_POST_STATUS_HIDDEN
	post.HiddenBy = msg.Creator
	post.HiddenAt = now

	// Store updated post
	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// Create HideRecord and update sentinel activity for non-gov senders
	if !isGovAuthority {
		slashAmount := math.NewInt(100) // DefaultSentinelSlashAmount

		// Get sentinel backing for snapshot
		backing := k.GetSentinelBacking(ctx, msg.Creator)

		// Create hide record
		hideRecord := types.HideRecord{
			PostId:                  msg.PostId,
			Sentinel:                msg.Creator,
			HiddenAt:                now,
			SentinelBondSnapshot:    sentinelActivity.CurrentBond,
			SentinelBackingSnapshot: backing.String(),
			CommittedAmount:         slashAmount.String(),
			ReasonCode:              reasonCode,
			ReasonText:              msg.ReasonText,
		}

		if err := k.HideRecord.Set(ctx, msg.PostId, hideRecord); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store hide record")
		}

		// Update sentinel activity
		committedBond, _ := math.NewIntFromString(sentinelActivity.TotalCommittedBond)
		if sentinelActivity.TotalCommittedBond == "" {
			committedBond = math.ZeroInt()
		}
		sentinelActivity.TotalCommittedBond = committedBond.Add(slashAmount).String()
		sentinelActivity.PendingHideCount++
		sentinelActivity.TotalHides++
		sentinelActivity.EpochHides++

		if err := k.SentinelActivity.Set(ctx, msg.Creator, sentinelActivity); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update sentinel activity")
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_hidden",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("hidden_by", msg.Creator),
			sdk.NewAttribute("reason_code", fmt.Sprintf("%d", msg.ReasonCode)),
			sdk.NewAttribute("is_gov_authority", fmt.Sprintf("%t", isGovAuthority)),
		),
	)

	return &types.MsgHidePostResponse{}, nil
}
