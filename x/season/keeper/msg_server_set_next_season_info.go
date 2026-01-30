package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetNextSeasonInfo sets the name and theme for the next season.
// Only Commons Council can set next season info.
// This info is used when the current season ends and a new one starts.
func (k msgServer) SetNextSeasonInfo(ctx context.Context, msg *types.MsgSetNextSeasonInfo) (*types.MsgSetNextSeasonInfoResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority (Commons Council or governance)
	if !k.IsCommonsCouncil(ctx, msg.Authority) {
		return nil, types.ErrNotCommonsCouncil
	}

	// Validate name length
	if len(msg.Name) == 0 {
		return nil, errorsmod.Wrap(types.ErrDisplayNameTooShort, "season name cannot be empty")
	}
	if len(msg.Name) > 100 {
		return nil, errorsmod.Wrap(types.ErrDisplayNameTooLong, "season name too long")
	}

	// Validate theme length
	if len(msg.Theme) > 200 {
		return nil, errorsmod.Wrap(types.ErrDisplayNameTooLong, "season theme too long")
	}

	// Get current season number for event
	season, err := k.Season.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get current season")
	}

	// Create next season info
	info := types.NextSeasonInfo{
		Name:  msg.Name,
		Theme: msg.Theme,
	}

	if err := k.NextSeasonInfo.Set(ctx, info); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save next season info")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"next_season_info_set",
			sdk.NewAttribute("current_season", fmt.Sprintf("%d", season.Number)),
			sdk.NewAttribute("next_season_name", msg.Name),
			sdk.NewAttribute("next_season_theme", msg.Theme),
			sdk.NewAttribute("set_by", msg.Authority),
		),
	)

	return &types.MsgSetNextSeasonInfoResponse{}, nil
}
