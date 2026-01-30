package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) IncreaseBounty(ctx context.Context, msg *types.MsgIncreaseBounty) (*types.MsgIncreaseBountyResponse, error) {
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
		return nil, errorsmod.Wrap(types.ErrNotBountyCreator, "only the bounty creator can increase it")
	}

	// Check bounty is active
	if bounty.Status != types.BountyStatus_BOUNTY_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrBountyNotActive, "bounty status is %s", bounty.Status.String())
	}

	// Parse and validate amount
	addAmount, ok := math.NewIntFromString(msg.AdditionalAmount)
	if !ok || addAmount.IsNegative() || addAmount.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid increase amount")
	}

	// TODO: Transfer additional SPARK from creator to module (escrow)

	// Update bounty amount
	currentAmount, _ := math.NewIntFromString(bounty.Amount)
	newAmount := currentAmount.Add(addAmount)
	bounty.Amount = newAmount.String()

	if err := k.Bounty.Set(ctx, msg.BountyId, bounty); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update bounty")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bounty_increased",
			sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", msg.BountyId)),
			sdk.NewAttribute("added_amount", msg.AdditionalAmount),
			sdk.NewAttribute("new_total", bounty.Amount),
		),
	)

	return &types.MsgIncreaseBountyResponse{}, nil
}
