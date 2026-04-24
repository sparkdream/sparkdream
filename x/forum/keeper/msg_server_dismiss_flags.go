package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) DismissFlags(ctx context.Context, msg *types.MsgDismissFlags) (*types.MsgDismissFlagsResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Only sentinels or operations committee can dismiss flags
	isGovAuthority := k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	// Check if sender is an active sentinel (not demoted) via x/rep.
	isSentinel := false
	if !isGovAuthority && k.repKeeper != nil {
		br, serr := k.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator)
		isSentinel = serr == nil &&
			br.CurrentBond != "" &&
			br.BondStatus != reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED
	}

	if !isGovAuthority && !isSentinel {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only sentinels or operations committee can dismiss flags")
	}

	// Load flag record
	postFlag, err := k.PostFlag.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrFlagNotFound, fmt.Sprintf("no flags for post %d", msg.PostId))
	}

	// Check if post is in review queue (sentinel can only dismiss posts in review queue)
	if !isGovAuthority && !postFlag.InReviewQueue {
		return nil, errorsmod.Wrap(types.ErrNotInReviewQueue, "only governance authority can dismiss posts not in review queue")
	}

	// Remove flag record
	if err := k.PostFlag.Remove(ctx, msg.PostId); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove flag record")
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"flags_dismissed",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("dismissed_by", msg.Creator),
			sdk.NewAttribute("is_gov_authority", fmt.Sprintf("%t", isGovAuthority)),
			sdk.NewAttribute("flag_count", fmt.Sprintf("%d", len(postFlag.Flaggers))),
		),
	)

	return &types.MsgDismissFlagsResponse{}, nil
}
