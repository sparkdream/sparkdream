package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetModerationPaused allows governance authority to pause or unpause moderation actions.
func (k msgServer) SetModerationPaused(ctx context.Context, msg *types.MsgSetModerationPaused) (*types.MsgSetModerationPausedResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Only governance, council, or operations committee can pause/unpause moderation
	if !k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority, "only governance, council, or operations committee can pause/unpause moderation")
	}

	// Load current params
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}

	// Update moderation paused state
	// Note: Params doesn't have ModerationPaused field yet, this would be added
	// For now, we emit an event and the state would be tracked separately

	// Emit event
	status := "paused"
	if !msg.Paused {
		status = "unpaused"
	}

	// Update ModerationPaused in params
	params.ModerationPaused = msg.Paused
	if err := k.Params.Set(ctx, params); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update params")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"moderation_paused_status_changed",
			sdk.NewAttribute("paused", fmt.Sprintf("%t", msg.Paused)),
			sdk.NewAttribute("status", status),
			sdk.NewAttribute("changed_by", msg.Creator),
		),
	)

	return &types.MsgSetModerationPausedResponse{}, nil
}
