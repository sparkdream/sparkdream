package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) MoveThread(ctx context.Context, msg *types.MsgMoveThread) (*types.MsgMoveThreadResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if params.ForumPaused {
		return nil, types.ErrForumPaused
	}

	post, err := k.Post.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.RootId))
	}

	if post.ParentId != 0 {
		return nil, types.ErrNotRootPost
	}

	if k.commonsKeeper == nil || !k.commonsKeeper.HasCategory(ctx, msg.NewCategoryId) {
		return nil, errorsmod.Wrap(types.ErrCategoryNotFound, fmt.Sprintf("category %d not found", msg.NewCategoryId))
	}

	if post.CategoryId == msg.NewCategoryId {
		return nil, errorsmod.Wrap(types.ErrInvalidCategoryId, "thread is already in this category")
	}

	originalCategoryId := post.CategoryId

	isGovAuthority := k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	var bondSnapshot string
	if !isGovAuthority {
		if params.ModerationPaused {
			return nil, types.ErrModerationPaused
		}

		if k.repKeeper != nil {
			for _, tag := range post.Tags {
				reserved, rerr := k.repKeeper.IsReservedTag(ctx, tag)
				if rerr == nil && reserved {
					return nil, errorsmod.Wrapf(types.ErrCannotMoveReservedTag,
						"thread has reserved tag '%s'", tag)
				}
			}
		}

		if k.repKeeper == nil {
			return nil, errorsmod.Wrap(types.ErrNotSentinel, "rep keeper not wired")
		}
		br, err := k.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator)
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrNotSentinel, "not a registered sentinel")
		}
		if _, ok := math.NewIntFromString(br.CurrentBond); !ok || br.CurrentBond == "" {
			return nil, errorsmod.Wrapf(types.ErrInvalidAmount, "invalid bonded role current_bond: %q", br.CurrentBond)
		}
		bondSnapshot = br.CurrentBond

		if br.BondStatus == reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED {
			return nil, types.ErrSentinelDemoted
		}

		local, err := k.SentinelActivity.Get(ctx, msg.Creator)
		if err != nil {
			local = types.SentinelActivity{Address: msg.Creator}
		}
		if local.OverturnCooldownUntil > now {
			return nil, errorsmod.Wrapf(types.ErrSentinelCooldown,
				"cooldown until %d", local.OverturnCooldownUntil)
		}
		if local.EpochMoves >= types.DefaultMaxSentinelMovesPerEpoch {
			return nil, types.ErrMoveLimitExceeded
		}

		if msg.Reason == "" {
			return nil, types.ErrMoveReasonRequired
		}

		backing := k.GetSentinelBacking(ctx, msg.Creator)

		// Reserve slash amount against the sentinel's bond so overturned
		// appeals have funds to slash. Mirrors the HidePost reservation path.
		slashAmount := math.NewInt(types.DefaultSentinelSlashAmount)
		if err := k.repKeeper.ReserveBond(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator, slashAmount); err != nil {
			return nil, errorsmod.Wrap(err, "insufficient bond to move")
		}

		moveRecord := types.ThreadMoveRecord{
			RootId:                  msg.RootId,
			Sentinel:                msg.Creator,
			OriginalCategoryId:      originalCategoryId,
			NewCategoryId:           msg.NewCategoryId,
			MovedAt:                 now,
			SentinelBondSnapshot:    bondSnapshot,
			SentinelBackingSnapshot: backing.String(),
			MoveReason:              msg.Reason,
			AppealPending:           false,
			InitiativeId:            0,
		}

		if err := k.ThreadMoveRecord.Set(ctx, msg.RootId, moveRecord); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store move record")
		}

		local.TotalMoves++
		local.EpochMoves++
		if err := k.SentinelActivity.Set(ctx, msg.Creator, local); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update sentinel activity")
		}

		_ = k.repKeeper.RecordActivity(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator)
	}

	post.CategoryId = msg.NewCategoryId

	if err := k.Post.Set(ctx, msg.RootId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

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
