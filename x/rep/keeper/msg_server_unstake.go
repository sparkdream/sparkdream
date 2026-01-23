package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) Unstake(ctx context.Context, msg *types.MsgUnstake) (*types.MsgUnstakeResponse, error) {
	stakerAddr, err := k.addressCodec.StringToBytes(msg.Staker)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid staker address")
	}

	// Get the stake to calculate returned amount
	stake, err := k.Keeper.GetStake(ctx, msg.StakeId)
	if err != nil {
		return nil, err
	}

	// Determine amount to remove (full stake if not specified)
	var amountToRemove math.Int
	if msg.Amount != nil && !msg.Amount.IsNil() && !msg.Amount.IsZero() {
		amountToRemove = *msg.Amount
	} else {
		amountToRemove = stake.Amount
	}

	// Calculate pending rewards before removing
	pendingRewards, err := k.Keeper.GetPendingStakingRewards(ctx, stake)
	if err != nil {
		pendingRewards = math.ZeroInt()
	}

	// Remove the stake
	if err := k.Keeper.RemoveStake(ctx, msg.StakeId, sdk.AccAddress(stakerAddr), amountToRemove); err != nil {
		return nil, err
	}

	return &types.MsgUnstakeResponse{
		ReturnedAmount: &amountToRemove,
		RewardAmount:   &pendingRewards,
	}, nil
}
