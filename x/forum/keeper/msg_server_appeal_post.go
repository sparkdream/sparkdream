package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) AppealPost(ctx context.Context, msg *types.MsgAppealPost) (*types.MsgAppealPostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// TODO: Check appeals_paused param

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

	// Check if a hide record exists (sentinel hide vs gov hide)
	hideRecord, err := k.HideRecord.Get(ctx, msg.PostId)
	if err != nil {
		// No hide record means governance authority hid this post
		// Gov hides must be appealed via MsgAppealGovAction
		return nil, errorsmod.Wrap(types.ErrGovLockNotAppealable,
			"governance authority hides must be appealed via governance action appeal")
	}

	// Check appeal cooldown
	cooldownEnd := hideRecord.HiddenAt + types.DefaultHideAppealCooldown
	if now < cooldownEnd {
		return nil, errorsmod.Wrapf(types.ErrAppealCooldown,
			"must wait until %d to appeal", cooldownEnd)
	}

	// TODO: Charge appeal_fee to appellant and escrow it

	// Create appeal initiative in x/rep (stub)
	payload, _ := json.Marshal(map[string]interface{}{
		"post_id":       msg.PostId,
		"sentinel_addr": hideRecord.Sentinel,
		"appellant_addr": msg.Creator,
		"reason_code":   hideRecord.ReasonCode,
		"reason_text":   hideRecord.ReasonText,
	})

	deadline := now + types.DefaultAppealDeadline
	initiativeID, err := k.CreateAppealInitiative(ctx, "POST_HIDE_APPEAL", payload, deadline)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create appeal initiative")
	}

	// Update sentinel activity to track pending appeal
	sentinelActivity, err := k.SentinelActivity.Get(ctx, hideRecord.Sentinel)
	if err == nil {
		sentinelActivity.EpochAppealsFiled++
		if err := k.SentinelActivity.Set(ctx, hideRecord.Sentinel, sentinelActivity); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update sentinel activity")
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_appeal_filed",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("appellant", msg.Creator),
			sdk.NewAttribute("sentinel", hideRecord.Sentinel),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("deadline", fmt.Sprintf("%d", deadline)),
		),
	)

	return &types.MsgAppealPostResponse{}, nil
}
