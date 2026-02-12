package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetForumPaused allows governance authority to pause or unpause the forum.
func (k msgServer) SetForumPaused(ctx context.Context, msg *types.MsgSetForumPaused) (*types.MsgSetForumPausedResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Only governance, council, or operations committee can pause/unpause
	if !k.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority, "only governance, council, or operations committee can pause/unpause the forum")
	}

	// Load current params
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}

	// Update paused state
	// Note: Params doesn't have ForumPaused field yet, this would be added
	// For now, we emit an event and the state would be tracked separately

	// Emit event
	status := "paused"
	if !msg.Paused {
		status = "unpaused"
	}

	// Update ForumPaused in params
	params.ForumPaused = msg.Paused
	if err := k.Params.Set(ctx, params); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update params")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"forum_paused_status_changed",
			sdk.NewAttribute("paused", fmt.Sprintf("%t", msg.Paused)),
			sdk.NewAttribute("status", status),
			sdk.NewAttribute("changed_by", msg.Creator),
		),
	)

	return &types.MsgSetForumPausedResponse{}, nil
}
