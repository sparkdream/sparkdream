package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	commontypes "sparkdream/x/common/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) HidePost(ctx context.Context, msg *types.MsgHidePost) (*types.MsgHidePostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if params.ModerationPaused {
		return nil, types.ErrModerationPaused
	}

	reasonCode := commontypes.ModerationReason(msg.ReasonCode)
	if reasonCode == commontypes.ModerationReason_MODERATION_REASON_UNSPECIFIED {
		return nil, types.ErrInvalidReasonCode
	}
	if reasonCode == commontypes.ModerationReason_MODERATION_REASON_OTHER && msg.ReasonText == "" {
		return nil, types.ErrReasonTextRequired
	}

	isGovAuthority := k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	// Rep-owned accountability state for non-gov senders.
	var (
		repSentinel reptypes.SentinelActivity
		bondSnapshot string
	)
	slashAmount := math.NewInt(types.DefaultSentinelSlashAmount)

	if !isGovAuthority {
		if k.repKeeper == nil {
			return nil, errorsmod.Wrap(types.ErrNotSentinel, "rep keeper not wired")
		}
		sa, err := k.repKeeper.GetSentinel(ctx, msg.Creator)
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrNotSentinel, "not a registered sentinel")
		}
		repSentinel = sa
		bondSnapshot = sa.CurrentBond

		if sa.BondStatus == reptypes.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED {
			return nil, types.ErrSentinelDemoted
		}

		// Forum-local cooldown + hide counter.
		local, err := k.SentinelActivity.Get(ctx, msg.Creator)
		if err != nil {
			local = types.SentinelActivity{Address: msg.Creator}
		}
		if local.OverturnCooldownUntil > now {
			return nil, errorsmod.Wrapf(types.ErrSentinelCooldown,
				"cooldown until %d", local.OverturnCooldownUntil)
		}
		if local.EpochHides >= types.DefaultMaxHidesPerEpoch {
			return nil, types.ErrHideLimitExceeded
		}

		// Reserve the slash amount out of available bond before committing.
		if err := k.repKeeper.ReserveBond(ctx, msg.Creator, slashAmount); err != nil {
			return nil, errorsmod.Wrap(err, "insufficient bond to hide")
		}
	}

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

	post.Status = types.PostStatus_POST_STATUS_HIDDEN
	post.HiddenBy = msg.Creator
	post.HiddenAt = now

	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	if !isGovAuthority {
		_ = repSentinel // bond snapshot captured above

		backing := k.GetSentinelBacking(ctx, msg.Creator)

		hideRecord := types.HideRecord{
			PostId:                  msg.PostId,
			Sentinel:                msg.Creator,
			HiddenAt:                now,
			SentinelBondSnapshot:    bondSnapshot,
			SentinelBackingSnapshot: backing.String(),
			CommittedAmount:         slashAmount.String(),
			ReasonCode:              reasonCode,
			ReasonText:              msg.ReasonText,
		}
		if err := k.HideRecord.Set(ctx, msg.PostId, hideRecord); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store hide record")
		}

		// Forum-local counters + pending-hide tracking.
		local, err := k.SentinelActivity.Get(ctx, msg.Creator)
		if err != nil {
			local = types.SentinelActivity{Address: msg.Creator}
		}
		local.PendingHideCount++
		local.TotalHides++
		local.EpochHides++
		if err := k.SentinelActivity.Set(ctx, msg.Creator, local); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update sentinel activity")
		}

		_ = k.repKeeper.RecordActivity(ctx, msg.Creator)
	}

	if k.repKeeper != nil {
		if err := k.repKeeper.SlashAuthorBond(ctx, reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, msg.PostId); err != nil {
			sdkCtx.Logger().Debug("author bond slash skipped", "post_id", msg.PostId, "error", err)
		}
	}

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
