package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
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

	// Resolve which moderation authority this move invokes. An account can be
	// BOTH a bonded sentinel and a Commons Operations Committee member, so the
	// choice must be explicit rather than an implicit upgrade to the council
	// path. See docs/x-forum-spec.md (Shared ModerationAuthority).
	//
	// Move eligibility = bonded sentinel in NORMAL/RECOVERY with a valid bond
	// AND the thread carries no reserved tag (only the council may move a
	// reserved-tag thread). Cooldown and the epoch limit are operational gates
	// applied once the sentinel path is taken, not part of the decision.
	var (
		bondSnapshot     string
		sentinelEligible bool
		sentinelErr      error
	)
	if k.repKeeper == nil {
		sentinelErr = errorsmod.Wrap(types.ErrNotSentinel, "rep keeper not wired")
	} else if br, berr := k.eligibleSentinel(ctx, msg.Creator); berr != nil {
		sentinelErr = berr
	} else if reservedTag := k.firstReservedTag(ctx, post.Tags); reservedTag != "" {
		sentinelErr = errorsmod.Wrapf(types.ErrCannotMoveReservedTag, "thread has reserved tag '%s'", reservedTag)
	} else {
		bondSnapshot = br.CurrentBond
		sentinelEligible = true
	}

	isCouncil := k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	isGovAuthority, err := resolveModerationAuthority(msg.Authority, sentinelEligible, sentinelErr, isCouncil)
	if err != nil {
		return nil, err
	}

	if !isGovAuthority {
		if params.ModerationPaused {
			return nil, types.ErrModerationPaused
		}

		// Shared cooldown + per-epoch move cap — both read from rep's
		// RoleActivity record (single source of truth).
		if until := k.repKeeper.RoleOverturnCooldownUntil(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator); until > now {
			return nil, errorsmod.Wrapf(types.ErrSentinelCooldown,
				"cooldown until %d", until)
		}
		if k.repKeeper.RoleEpochActionCount(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator, reptypes.ActionKindForumMove) >= params.MaxSentinelMovesPerEpoch {
			return nil, types.ErrMoveLimitExceeded
		}

		if msg.Reason == "" {
			return nil, types.ErrMoveReasonRequired
		}

		backing := k.GetSentinelBacking(ctx, msg.Creator)

		// Reserve slash amount against the sentinel's bond so overturned
		// appeals have funds to slash. Mirrors the HidePost reservation path.
		slashAmount := params.SentinelSlashAmountInt()
		if err := k.repKeeper.ReserveBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator, slashAmount); err != nil {
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
			CommittedAmount:         slashAmount.String(),
		}

		if err := k.ThreadMoveRecord.Set(ctx, msg.RootId, moveRecord); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store move record")
		}

		_ = k.repKeeper.RecordRoleAction(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator, reptypes.ActionKindForumMove)

		_ = k.repKeeper.RecordActivity(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, msg.Creator)
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

// firstReservedTag returns the first tag in tags that the rep registry marks as
// reserved, or "" if none are (or the rep keeper is unwired). Only the council
// may move a thread carrying a reserved tag, so a non-empty result makes the
// caller ineligible for the sentinel move path.
func (k msgServer) firstReservedTag(ctx context.Context, tags []string) string {
	if k.repKeeper == nil {
		return ""
	}
	for _, tag := range tags {
		if reserved, rerr := k.repKeeper.IsReservedTag(ctx, tag); rerr == nil && reserved {
			return tag
		}
	}
	return ""
}
