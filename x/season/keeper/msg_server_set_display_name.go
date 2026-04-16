package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetDisplayName sets the display name for a member profile.
// Display names are non-unique cosmetic names with a change cooldown.
func (k msgServer) SetDisplayName(ctx context.Context, msg *types.MsgSetDisplayName) (*types.MsgSetDisplayNameResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Validate display name
	// TODO: Consider adding content filtering (profanity, impersonation, unicode abuse)
	// to display names. Currently only length constraints are enforced. The existing
	// DisplayNameModeration system handles post-hoc reporting but proactive filtering
	// would reduce moderation load.
	if err := k.ValidateDisplayName(ctx, msg.Name); err != nil {
		return nil, err
	}

	// Get or create member profile
	profile, err := k.MemberProfile.Get(ctx, msg.Creator)
	if err != nil {
		// Create new profile if it doesn't exist
		profile = types.MemberProfile{
			Address:     msg.Creator,
			DisplayName: msg.Name,
			SeasonLevel: 1,
		}
	} else {
		// Check cooldown for existing profile
		params, err := k.Params.Get(ctx)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to get params")
		}

		currentEpoch := k.GetCurrentEpoch(ctx)
		if profile.LastDisplayNameChangeEpoch > 0 {
			epochsSinceChange := currentEpoch - profile.LastDisplayNameChangeEpoch
			if uint64(epochsSinceChange) < params.DisplayNameChangeCooldownEpochs {
				return nil, errorsmod.Wrapf(types.ErrDisplayNameCooldown,
					"must wait %d more epochs", params.DisplayNameChangeCooldownEpochs-uint64(epochsSinceChange))
			}
		}

		// Check if display name is currently moderated
		moderation, err := k.DisplayNameModeration.Get(ctx, msg.Creator)
		if err == nil && moderation.Active {
			return nil, errorsmod.Wrap(types.ErrDisplayNameModerated, "display name is currently moderated")
		}

		profile.DisplayName = msg.Name
		profile.LastDisplayNameChangeEpoch = currentEpoch
	}

	// Update last active epoch
	profile.LastActiveEpoch = k.GetCurrentEpoch(ctx)

	if err := k.MemberProfile.Set(ctx, msg.Creator, profile); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save profile")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"display_name_changed",
			sdk.NewAttribute("member", msg.Creator),
			sdk.NewAttribute("display_name", msg.Name),
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", k.GetCurrentEpoch(ctx))),
		),
	)

	return &types.MsgSetDisplayNameResponse{}, nil
}
