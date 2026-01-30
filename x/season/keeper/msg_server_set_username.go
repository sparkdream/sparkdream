package keeper

import (
	"context"
	"fmt"
	"strings"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetUsername sets a unique username for a member profile.
// Usernames are unique, reserved via x/name, and cost DREAM to set.
func (k msgServer) SetUsername(ctx context.Context, msg *types.MsgSetUsername) (*types.MsgSetUsernameResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	username := strings.ToLower(msg.Username)

	// Validate username format
	if err := k.ValidateUsername(ctx, username); err != nil {
		return nil, err
	}

	// Check username uniqueness by scanning all profiles
	iter, err := k.MemberProfile.Iterate(ctx, nil)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to iterate profiles")
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		profile, err := iter.Value()
		if err != nil {
			continue
		}
		if profile.Address != msg.Creator && strings.ToLower(profile.Username) == username {
			return nil, types.ErrUsernameAlreadyTaken
		}
	}

	// Get member profile (must exist)
	profile, err := k.MemberProfile.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrProfileNotFound, "member profile not found")
	}

	// Check cooldown
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	currentEpoch := k.GetCurrentEpoch(ctx)
	if profile.LastUsernameChangeEpoch > 0 {
		epochsSinceChange := currentEpoch - profile.LastUsernameChangeEpoch
		if uint64(epochsSinceChange) < params.UsernameChangeCooldownEpochs {
			return nil, errorsmod.Wrapf(types.ErrUsernameCooldown,
				"must wait %d more epochs", params.UsernameChangeCooldownEpochs-uint64(epochsSinceChange))
		}
	}

	// TODO: Charge DREAM cost via x/rep integration
	// k.repKeeper.BurnDREAM(ctx, msg.Creator, params.UsernameCostDream)

	// TODO: Reserve username via x/name integration
	// k.nameKeeper.ReserveName(ctx, username, NameTypeUsername, msg.Creator)

	// Release old username if set
	// if profile.Username != "" {
	//     k.nameKeeper.ReleaseName(ctx, profile.Username)
	// }

	oldUsername := profile.Username
	profile.Username = username
	profile.LastUsernameChangeEpoch = currentEpoch
	profile.LastActiveEpoch = currentEpoch

	if err := k.MemberProfile.Set(ctx, msg.Creator, profile); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save profile")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"username_changed",
			sdk.NewAttribute("member", msg.Creator),
			sdk.NewAttribute("old_username", oldUsername),
			sdk.NewAttribute("new_username", username),
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", currentEpoch)),
		),
	)

	return &types.MsgSetUsernameResponse{}, nil
}
