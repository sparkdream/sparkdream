package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AwardBounty finalizes a bounty by transferring escrowed funds to recipients and marking it as awarded
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
	if !k.canActAs(ctx, msg.Creator, bounty.Creator) {
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

	// Split the escrow equally among the accepted replies that are still
	// eligible (ACTIVE, not anonymous).
	paid, err := k.payBountyAwards(ctx, &bounty)
	if err != nil {
		return nil, err
	}
	if paid == 0 {
		return nil, errorsmod.Wrap(types.ErrBountyNotActive, "no assigned reply is still eligible for an award")
	}

	// Mark bounty as awarded
	bounty.Status = types.BountyStatus_BOUNTY_STATUS_AWARDED

	if err := k.Bounty.Set(ctx, msg.BountyId, bounty); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update bounty")
	}
	// FORUM-S2-8: drop from BountiesByExpiry — awarded bounties are no longer
	// candidates for the BountyExpiringSoon query.
	if err := k.BountiesByExpiry.Remove(ctx, collections.Join(bounty.ExpiresAt, bounty.Id)); err != nil {
		return nil, err
	}
	k.clearActiveBountyIndex(ctx, bounty)

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bounty_awarded",
			sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", msg.BountyId)),
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", bounty.ThreadId)),
			sdk.NewAttribute("creator", msg.Creator),
			sdk.NewAttribute("total_awards", fmt.Sprintf("%d", len(bounty.Awards))),
			sdk.NewAttribute("amount", bounty.Amount),
		),
	)

	return &types.MsgAwardBountyResponse{}, nil
}
