package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CancelBounty(ctx context.Context, msg *types.MsgCancelBounty) (*types.MsgCancelBountyResponse, error) {
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
		return nil, errorsmod.Wrap(types.ErrNotBountyCreator, "only the bounty creator can cancel it")
	}

	// Check bounty is active
	if bounty.Status != types.BountyStatus_BOUNTY_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrBountyNotActive, "bounty status is %s", bounty.Status.String())
	}

	// Check no awards have been made
	if len(bounty.Awards) > 0 {
		return nil, errorsmod.Wrap(types.ErrBountyAlreadyAwarded, "cannot cancel bounty with existing awards")
	}

	// Refund SPARK from escrow to creator (minus cancellation fee)
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}

	bountyAmount, ok := math.NewIntFromString(bounty.Amount)
	if !ok {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid bounty amount")
	}

	// Calculate cancellation fee
	feePercent := params.BountyCancellationFeePercent
	if feePercent > 100 {
		feePercent = 100
	}
	cancellationFee := bountyAmount.Mul(math.NewInt(int64(feePercent))).Quo(math.NewInt(100))
	refundAmount := bountyAmount.Sub(cancellationFee)

	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)

	// Refund to creator (minus fee)
	if refundAmount.IsPositive() {
		refundCoins := sdk.NewCoins(sdk.NewCoin(types.DefaultFeeDenom, refundAmount))
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, creatorAddr, refundCoins); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund bounty")
		}
	}

	// Burn the cancellation fee
	if cancellationFee.IsPositive() {
		feeCoins := sdk.NewCoins(sdk.NewCoin(types.DefaultFeeDenom, cancellationFee))
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, feeCoins); err != nil {
			return nil, errorsmod.Wrap(err, "failed to burn cancellation fee")
		}
	}

	// Update bounty status
	bounty.Status = types.BountyStatus_BOUNTY_STATUS_CANCELLED

	if err := k.Bounty.Set(ctx, msg.BountyId, bounty); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update bounty")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bounty_cancelled",
			sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", msg.BountyId)),
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", bounty.ThreadId)),
			sdk.NewAttribute("amount_refunded", bounty.Amount),
		),
	)

	return &types.MsgCancelBountyResponse{}, nil
}
