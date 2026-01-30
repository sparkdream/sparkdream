package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ReportDisplayName reports a display name for moderation.
// The reporter must stake DREAM which is burned if the report is frivolous.
func (k msgServer) ReportDisplayName(ctx context.Context, msg *types.MsgReportDisplayName) (*types.MsgReportDisplayNameResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid reporter address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Target); err != nil {
		return nil, errorsmod.Wrap(err, "invalid target address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Cannot report own display name
	if msg.Creator == msg.Target {
		return nil, types.ErrCannotReportOwnDisplayName
	}

	// Get target's profile
	profile, err := k.MemberProfile.Get(ctx, msg.Target)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrProfileNotFound, "target profile not found")
	}

	// Check target has a display name
	if profile.DisplayName == "" {
		return nil, errorsmod.Wrap(types.ErrDisplayNameTooShort, "target has no display name")
	}

	// Check target is not already moderated
	existingModeration, err := k.DisplayNameModeration.Get(ctx, msg.Target)
	if err == nil && existingModeration.Active {
		return nil, types.ErrDisplayNameModerated
	}

	// Get params for stake amount
	params, _ := k.Params.Get(ctx)

	// TODO: Escrow reporter's DREAM stake
	// k.repKeeper.EscrowDREAM(ctx, msg.Creator, params.DisplayNameReportStakeDream)

	// Generate challenge ID
	challengeID := fmt.Sprintf("dn:%s:%d", msg.Target, sdkCtx.BlockHeight())

	// Create moderation record
	moderation := types.DisplayNameModeration{
		Member:       msg.Target,
		RejectedName: profile.DisplayName,
		Reason:       msg.Reason,
		ModeratedAt:  sdkCtx.BlockHeight(),
		Active:       true,
	}

	if err := k.DisplayNameModeration.Set(ctx, msg.Target, moderation); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save moderation")
	}

	// Clear the target's display name (moderated)
	profile.DisplayName = ""
	if err := k.MemberProfile.Set(ctx, msg.Target, profile); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update profile")
	}

	// Store reporter stake record
	stakeRecord := types.DisplayNameReportStake{
		ChallengeId: challengeID,
		Reporter:    msg.Creator,
		Amount:      params.DisplayNameReportStakeDream,
	}
	if err := k.DisplayNameReportStake.Set(ctx, challengeID, stakeRecord); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save stake record")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"display_name_reported",
			sdk.NewAttribute("reporter", msg.Creator),
			sdk.NewAttribute("target", msg.Target),
			sdk.NewAttribute("rejected_name", moderation.RejectedName),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("challenge_id", challengeID),
		),
	)

	return &types.MsgReportDisplayNameResponse{}, nil
}
