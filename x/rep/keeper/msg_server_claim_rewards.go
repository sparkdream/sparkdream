package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ClaimStakingRewards claims pending rewards for a stake
func (k msgServer) ClaimStakingRewards(ctx context.Context, msg *types.MsgClaimStakingRewards) (*types.MsgClaimStakingRewardsResponse, error) {
	stakerAddr, err := k.addressCodec.StringToBytes(msg.Staker)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid staker address")
	}

	// Claim the rewards
	claimedAmount, err := k.Keeper.ClaimStakingRewards(ctx, msg.StakeId, sdk.AccAddress(stakerAddr))
	if err != nil {
		return nil, err
	}

	return &types.MsgClaimStakingRewardsResponse{
		ClaimedAmount: &claimedAmount,
	}, nil
}

// CompoundStakingRewards compounds pending rewards into the stake principal
func (k msgServer) CompoundStakingRewards(ctx context.Context, msg *types.MsgCompoundStakingRewards) (*types.MsgCompoundStakingRewardsResponse, error) {
	stakerAddr, err := k.addressCodec.StringToBytes(msg.Staker)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid staker address")
	}

	// Compound the rewards
	compoundedAmount, err := k.Keeper.CompoundStakingRewards(ctx, msg.StakeId, sdk.AccAddress(stakerAddr))
	if err != nil {
		return nil, err
	}

	// Get updated stake for new amount
	stake, err := k.Keeper.GetStake(ctx, msg.StakeId)
	if err != nil {
		return nil, err
	}

	newAmount := stake.Amount

	return &types.MsgCompoundStakingRewardsResponse{
		CompoundedAmount: &compoundedAmount,
		NewStakeAmount:   &newAmount,
	}, nil
}
