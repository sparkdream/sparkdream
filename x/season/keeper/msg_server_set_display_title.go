package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetDisplayTitle sets the displayed title for a member profile.
// The member must have the title unlocked to display it.
func (k msgServer) SetDisplayTitle(ctx context.Context, msg *types.MsgSetDisplayTitle) (*types.MsgSetDisplayTitleResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get member profile (must exist)
	profile, err := k.MemberProfile.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrProfileNotFound, "member profile not found")
	}

	// If clearing the title (empty string), allow it
	if msg.TitleId == "" {
		profile.DisplayTitle = ""
	} else {
		// Check if the title exists
		_, err := k.Title.Get(ctx, msg.TitleId)
		if err != nil {
			return nil, errorsmod.Wrapf(types.ErrTitleNotFound, "title %s not found", msg.TitleId)
		}

		// Check if the member has unlocked this title
		if !k.HasUnlockedTitle(ctx, msg.Creator, msg.TitleId) {
			return nil, errorsmod.Wrapf(types.ErrTitleNotUnlocked, "title %s not unlocked", msg.TitleId)
		}

		profile.DisplayTitle = msg.TitleId
	}

	// Update last active epoch
	profile.LastActiveEpoch = k.GetCurrentEpoch(ctx)

	if err := k.MemberProfile.Set(ctx, msg.Creator, profile); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save profile")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"display_title_changed",
			sdk.NewAttribute("member", msg.Creator),
			sdk.NewAttribute("title_id", msg.TitleId),
		),
	)

	return &types.MsgSetDisplayTitleResponse{}, nil
}
