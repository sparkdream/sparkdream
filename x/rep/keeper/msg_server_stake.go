package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) Stake(ctx context.Context, msg *types.MsgStake) (*types.MsgStakeResponse, error) {
	stakerAddr, err := k.addressCodec.StringToBytes(msg.Staker)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid staker address")
	}

	// Validate amount is provided
	if msg.Amount == nil || msg.Amount.IsNil() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "amount is required")
	}

	// Dereference the pointer for the keeper call
	amount := *msg.Amount

	// Create the stake with target_identifier for member/tag staking
	stakeID, err := k.Keeper.CreateStake(ctx, sdk.AccAddress(stakerAddr), msg.TargetType, msg.TargetId, msg.TargetIdentifier, amount)
	if err != nil {
		return nil, err
	}

	return &types.MsgStakeResponse{StakeId: stakeID}, nil
}
