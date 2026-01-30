package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AwardBounty marks a bounty as awarded (simplified - actual award happens via AssignBountyToReply)
func (k msgServer) AwardBounty(ctx context.Context, msg *types.MsgAwardBounty) (*types.MsgAwardBountyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Load bounty
	bounty, err := k.Bounty.Get(ctx, msg.BountyId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrBountyNotFound, fmt.Sprintf("bounty %d not found", msg.BountyId))
	}

	// Verify creator is the bounty creator
	if bounty.Creator != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotBountyCreator, "only the bounty creator can award it")
	}

	// Check bounty is active
	if bounty.Status != types.BountyStatus_BOUNTY_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrBountyNotActive, "bounty status is %s", bounty.Status.String())
	}

	// Check bounty has awards
	if len(bounty.Awards) == 0 {
		return nil, errorsmod.Wrap(types.ErrBountyNotActive, "no awards assigned yet - use AssignBountyToReply first")
	}

	// Mark bounty as awarded
	bounty.Status = types.BountyStatus_BOUNTY_STATUS_AWARDED

	if err := k.Bounty.Set(ctx, msg.BountyId, bounty); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update bounty")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bounty_awarded",
			sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", msg.BountyId)),
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", bounty.ThreadId)),
			sdk.NewAttribute("creator", msg.Creator),
			sdk.NewAttribute("total_awards", fmt.Sprintf("%d", len(bounty.Awards))),
		),
	)

	return &types.MsgAwardBountyResponse{}, nil
}
