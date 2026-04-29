package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
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

	// Reject when assigned awards exceed the escrowed bounty amount — prevents
	// over-payout if award assignment logic allowed drift.
	totalAwards := math.ZeroInt()
	for _, award := range bounty.Awards {
		awardAmt, ok := math.NewIntFromString(award.Amount)
		if !ok || !awardAmt.IsPositive() {
			continue
		}
		totalAwards = totalAwards.Add(awardAmt)
	}
	bountyAmount, ok := math.NewIntFromString(bounty.Amount)
	if !ok {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid bounty escrow amount")
	}
	if totalAwards.GT(bountyAmount) {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "awards exceed bounty escrow")
	}

	// Transfer escrowed funds to each award recipient
	for _, award := range bounty.Awards {
		awardAmount, ok := math.NewIntFromString(award.Amount)
		if !ok || !awardAmount.IsPositive() {
			continue
		}
		recipientAddr, err := sdk.AccAddressFromBech32(award.Recipient)
		if err != nil {
			return nil, errorsmod.Wrapf(err, "invalid recipient address for award to post %d", award.PostId)
		}
		awardCoins := sdk.NewCoins(sdk.NewCoin(types.DefaultFeeDenom, awardAmount))
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipientAddr, awardCoins); err != nil {
			return nil, errorsmod.Wrapf(err, "failed to transfer bounty award to %s", award.Recipient)
		}
	}

	// Mark bounty as awarded
	bounty.Status = types.BountyStatus_BOUNTY_STATUS_AWARDED

	if err := k.Bounty.Set(ctx, msg.BountyId, bounty); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update bounty")
	}
	// FORUM-S2-8: drop from BountiesByExpiry — awarded bounties are no longer
	// candidates for the BountyExpiringSoon query.
	_ = k.BountiesByExpiry.Remove(ctx, collections.Join(bounty.ExpiresAt, bounty.Id))

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
