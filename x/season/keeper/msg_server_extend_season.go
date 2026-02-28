package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ExtendSeason extends the current season by a specified number of epochs.
// Only Commons Council can extend the season.
func (k msgServer) ExtendSeason(ctx context.Context, msg *types.MsgExtendSeason) (*types.MsgExtendSeasonResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority (Commons Council or governance)
	if !k.IsCommonsCouncil(ctx, msg.Authority) {
		return nil, types.ErrNotCommonsCouncil
	}

	// Get current season
	season, err := k.Season.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get current season")
	}

	// Check season is active or in nomination phase (extending during nomination is valid)
	if season.Status != types.SeasonStatus_SEASON_STATUS_ACTIVE &&
		season.Status != types.SeasonStatus_SEASON_STATUS_NOMINATION {
		return nil, types.ErrSeasonNotActive
	}

	// Get params
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Check max extensions
	if season.ExtensionsCount >= uint64(params.MaxSeasonExtensions) {
		return nil, types.ErrMaxExtensionsReached
	}

	// Check extension amount
	if msg.ExtensionEpochs > params.MaxExtensionEpochs {
		return nil, errorsmod.Wrapf(types.ErrExtensionTooLong,
			"requested %d epochs, max is %d", msg.ExtensionEpochs, params.MaxExtensionEpochs)
	}

	if msg.ExtensionEpochs == 0 {
		return nil, errorsmod.Wrap(types.ErrExtensionTooLong, "extension must be at least 1 epoch")
	}

	// Store original end block if this is the first extension
	if season.OriginalEndBlock == 0 {
		season.OriginalEndBlock = season.EndBlock
	}

	// Calculate extension in blocks
	extensionBlocks := int64(msg.ExtensionEpochs) * params.EpochBlocks

	// Extend the season
	season.EndBlock += extensionBlocks
	season.ExtensionsCount++
	season.TotalExtensionEpochs += msg.ExtensionEpochs

	if err := k.Season.Set(ctx, season); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update season")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"season_extended",
			sdk.NewAttribute("season_number", fmt.Sprintf("%d", season.Number)),
			sdk.NewAttribute("extension_epochs", fmt.Sprintf("%d", msg.ExtensionEpochs)),
			sdk.NewAttribute("new_end_block", fmt.Sprintf("%d", season.EndBlock)),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("extended_by", msg.Authority),
		),
	)

	return &types.MsgExtendSeasonResponse{}, nil
}
