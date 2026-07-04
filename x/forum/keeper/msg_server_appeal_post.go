package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) AppealPost(ctx context.Context, msg *types.MsgAppealPost) (*types.MsgAppealPostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Check appeals_paused param
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if params.AppealsPaused {
		return nil, types.ErrAppealsPaused
	}

	// Load post
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	// Check post is hidden
	if post.Status != types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrPostNotHidden, "can only appeal hidden posts")
	}

	// Verify appellant is the post author
	if post.Author != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotPostAuthor, "only the post author can appeal")
	}

	// Check if a hide record exists and was created by a sentinel. Gov-
	// authority hides write a minimal HideRecord with Sentinel == "" so the
	// council can later restore the slashed author bond on unhide — but
	// they're still not appealable via this path (gov hides must use
	// MsgAppealGovAction).
	hideRecord, err := k.HideRecord.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrGovLockNotAppealable,
			"no hide record found — content was not hidden by a sentinel")
	}
	if hideRecord.Sentinel == "" {
		return nil, errorsmod.Wrap(types.ErrGovLockNotAppealable,
			"governance authority hides must be appealed via governance action appeal")
	}

	// Check appeal cooldown
	cooldownEnd := hideRecord.HiddenAt + params.HideAppealCooldown
	if now < cooldownEnd {
		return nil, errorsmod.Wrapf(types.ErrAppealCooldown,
			"must wait until %d to appeal", cooldownEnd)
	}

	// Open a moderation appeal through the unified x/rep GovActionAppeal path
	// (ActionType POST_HIDE, ActionTarget = post id). This charges the rep
	// appeal bond and creates a jury initiative + resolvable GovActionAppeal
	// record — the same machinery lock/move/pin disputes use. The legacy
	// per-action forum appeal fee is no longer charged; the rep bond
	// (refunded/burned per verdict) supersedes it.
	if k.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrGovLockNotAppealable, "rep keeper not wired")
	}
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	_, initiativeID, err := k.repKeeper.CreateGovActionAppeal(
		ctx,
		reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE,
		fmt.Sprintf("%d", msg.PostId),
		creatorAddr,
		fmt.Sprintf("hide appeal: %s", hideRecord.ReasonText),
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create hide appeal")
	}

	// Mark the hide as appealed so the hide-expiry EndBlocker does not later
	// count it as unchallenged (it will be tallied as upheld/overturned instead).
	hideRecord.Appealed = true
	if err := k.HideRecord.Set(ctx, msg.PostId, hideRecord); err != nil {
		return nil, errorsmod.Wrap(err, "failed to mark hide record appealed")
	}

	// Count the appeal against the sentinel on rep's shared RoleActivity
	// record (feeds the reward Gate 4 appeal-rate check).
	if k.repKeeper != nil {
		_ = k.repKeeper.RecordRoleAction(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, hideRecord.Sentinel, reptypes.ActionKindForumAppealFiled)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_appeal_filed",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("appellant", msg.Creator),
			sdk.NewAttribute("sentinel", hideRecord.Sentinel),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
		),
	)

	return &types.MsgAppealPostResponse{}, nil
}
