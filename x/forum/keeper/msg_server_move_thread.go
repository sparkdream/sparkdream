package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) MoveThread(ctx context.Context, msg *types.MsgMoveThread) (*types.MsgMoveThreadResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Check forum_paused param
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if params.ForumPaused {
		return nil, types.ErrForumPaused
	}

	// Load post
	post, err := k.Post.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.RootId))
	}

	// Check this is a root post
	if post.ParentId != 0 {
		return nil, types.ErrNotRootPost
	}

	// Check new category exists
	_, err = k.Category.Get(ctx, msg.NewCategoryId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCategoryNotFound, fmt.Sprintf("category %d not found", msg.NewCategoryId))
	}

	// Check not moving to same category
	if post.CategoryId == msg.NewCategoryId {
		return nil, errorsmod.Wrap(types.ErrInvalidCategoryId, "thread is already in this category")
	}

	originalCategoryId := post.CategoryId

	// Check if sender is operations committee or sentinel
	isGovAuthority := k.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	if !isGovAuthority {
		// Check moderation_paused param for sentinels
		if params.ModerationPaused {
			return nil, types.ErrModerationPaused
		}

		// Sentinels cannot move threads with reserved tags
		for _, tag := range post.Tags {
			_, err := k.ReservedTag.Get(ctx, tag)
			if err == nil {
				return nil, errorsmod.Wrapf(types.ErrCannotMoveReservedTag,
					"thread has reserved tag '%s'", tag)
			}
		}

		// Load sentinel activity
		sentinelActivity, err := k.SentinelActivity.Get(ctx, msg.Creator)
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

		// Check move limit
		if sentinelActivity.EpochMoves >= types.DefaultMaxSentinelMovesPerEpoch {
			return nil, types.ErrMoveLimitExceeded
		}

		// Reason required for sentinels
		if msg.Reason == "" {
			return nil, types.ErrMoveReasonRequired
		}

		// Get backing for snapshot
		backing := k.GetSentinelBacking(ctx, msg.Creator)

		// Create move record for appeal tracking
		moveRecord := types.ThreadMoveRecord{
			RootId:                  msg.RootId,
			Sentinel:                msg.Creator,
			OriginalCategoryId:      originalCategoryId,
			NewCategoryId:           msg.NewCategoryId,
			MovedAt:                 now,
			SentinelBondSnapshot:    sentinelActivity.CurrentBond,
			SentinelBackingSnapshot: backing.String(),
			MoveReason:              msg.Reason,
			AppealPending:           false,
			InitiativeId:            0,
		}

		if err := k.ThreadMoveRecord.Set(ctx, msg.RootId, moveRecord); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store move record")
		}

		// Update sentinel activity
		sentinelActivity.TotalMoves++
		sentinelActivity.EpochMoves++

		if err := k.SentinelActivity.Set(ctx, msg.Creator, sentinelActivity); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update sentinel activity")
		}
	}

	// Update post category
	post.CategoryId = msg.NewCategoryId

	if err := k.Post.Set(ctx, msg.RootId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_moved",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.RootId)),
			sdk.NewAttribute("from_category", fmt.Sprintf("%d", originalCategoryId)),
			sdk.NewAttribute("to_category", fmt.Sprintf("%d", msg.NewCategoryId)),
			sdk.NewAttribute("moved_by", msg.Creator),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("is_gov_authority", fmt.Sprintf("%t", isGovAuthority)),
		),
	)

	return &types.MsgMoveThreadResponse{}, nil
}
