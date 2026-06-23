package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	commontypes "sparkdream/x/common/types"

	errorsmod "cosmossdk.io/errors"
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

	// Resolve which moderation authority this hide invokes. An account can be
	// BOTH a bonded sentinel and a Commons Operations Committee member, so the
	// choice must be explicit rather than an implicit upgrade to the cheaper,
	// less-appealable council path. See
	// docs/HANDOFF_HIDE_AUTHORITY_DISAMBIGUATION.md.
	//
	// "Eligible sentinel" = holds a ROLE_TYPE_FORUM_SENTINEL bond that can back
	// this action. NORMAL/RECOVERY are eligible outright; an UNBONDING role is
	// eligible while its staying bond (current - pending) covers min_sentinel_bond
	// — the action's ReserveBond below separately enforces the slash fits in that
	// staying bond. DEMOTED is never eligible. See eligibleSentinel.
	var (
		repSentinel      reptypes.BondedRole
		bondSnapshot     string
		sentinelEligible bool
		// sentinelErr explains why the sentinel path is unavailable; surfaced
		// for explicit SENTINEL requests and for the AUTO no-fallback case.
		sentinelErr error
	)
	slashAmount := params.SentinelSlashAmountInt()

	if br, serr := k.eligibleSentinel(ctx, msg.Creator); serr != nil {
		sentinelErr = serr
	} else {
		repSentinel = br
		bondSnapshot = br.CurrentBond
		sentinelEligible = true
	}

	isCouncil := k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	// isGovAuthority is the resolved decision: take the gov (council) path.
	isGovAuthority, err := resolveModerationAuthority(msg.Authority, sentinelEligible, sentinelErr, isCouncil)
	if err != nil {
		return nil, err
	}

	if !isGovAuthority {
		// Forum-local cooldown + hide counter.
		local, err := k.SentinelActivity.Get(ctx, msg.Creator)
		if err != nil {
			local = types.SentinelActivity{Address: msg.Creator}
		}
		if local.OverturnCooldownUntil > now {
			return nil, errorsmod.Wrapf(types.ErrSentinelCooldown,
				"cooldown until %d", local.OverturnCooldownUntil)
		}
		if local.EpochHides >= params.MaxHidesPerEpoch {
			return nil, types.ErrHideLimitExceeded
		}

		// Reserve the slash amount out of available bond before committing.
		if err := k.repKeeper.ReserveBond(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator, slashAmount); err != nil {
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

	// Snapshot the author bond amount BEFORE SlashAuthorBond runs below.
	// Reversal paths (MsgUnhidePost, appeal-OVERTURNED) read this back to
	// mint+re-lock the equivalent bond, making the slash→restore round-trip
	// net-zero on DREAM supply. Empty string when the post has no author
	// bond attached (the optional CreateAuthorBond branch in MsgCreatePost
	// was skipped) — a no-op on restore. Captured for BOTH sentinel and
	// gov-authority paths so council unhides can also restore the bond.
	authorBondAmount := ""
	if k.repKeeper != nil {
		if bond, err := k.repKeeper.GetAuthorBond(ctx, reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, msg.PostId); err == nil {
			authorBondAmount = bond.Amount.String()
		}
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
			AuthorBondAmount:        authorBondAmount,
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

		_ = k.repKeeper.RecordActivity(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator)
	} else {
		// Gov-authority hide: write a minimal HideRecord with Sentinel == ""
		// as the gov-hide marker. The empty Sentinel field is what
		// distinguishes gov hides from sentinel hides for AppealPost (which
		// rejects gov hides as ErrGovLockNotAppealable) and for sentinel-
		// counter callbacks (which soft-skip on missing sentinel). Only the
		// fields needed for council-driven reversal are populated.
		govRecord := types.HideRecord{
			PostId:           msg.PostId,
			Sentinel:         "", // gov-hide marker
			HiddenAt:         now,
			ReasonCode:       reasonCode,
			ReasonText:       msg.ReasonText,
			AuthorBondAmount: authorBondAmount,
		}
		if err := k.HideRecord.Set(ctx, msg.PostId, govRecord); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store gov-hide record")
		}
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
