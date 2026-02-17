package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/reveal/types"
)

func (k msgServer) Withdraw(ctx context.Context, msg *types.MsgWithdraw) (*types.MsgWithdrawResponse, error) {
	stakerAddr, err := k.addressCodec.StringToBytes(msg.Staker)
	if err != nil {
		return nil, types.ErrNotMember.Wrapf("invalid staker address: %s", err)
	}

	// Get stake
	stake, err := k.RevealStake.Get(ctx, msg.StakeId)
	if err != nil {
		return nil, types.ErrStakeNotFound.Wrapf("stake %d", msg.StakeId)
	}

	// Verify caller owns the stake
	if stake.Staker != msg.Staker {
		return nil, types.ErrUnauthorized.Wrapf("stake belongs to %s, not %s", stake.Staker, msg.Staker)
	}

	// Get contribution and tranche
	contrib, err := k.Contribution.Get(ctx, stake.ContributionId)
	if err != nil {
		return nil, types.ErrContributionNotFound
	}
	tranche, err := GetTranche(&contrib, stake.TrancheId)
	if err != nil {
		return nil, err
	}

	// Check withdrawal rules based on tranche status
	switch tranche.Status {
	case types.TrancheStatus_TRANCHE_STATUS_STAKING,
		types.TrancheStatus_TRANCHE_STATUS_BACKED:
		// Allowed
	case types.TrancheStatus_TRANCHE_STATUS_REVEALED,
		types.TrancheStatus_TRANCHE_STATUS_DISPUTED:
		return nil, types.ErrWithdrawalNotAllowed.Wrapf("tranche is in %s status", tranche.Status)
	default:
		// VERIFIED, CANCELLED, FAILED, LOCKED - stakes are auto-returned
		return nil, types.ErrWithdrawalNotAllowed.Wrapf("tranche is in %s status", tranche.Status)
	}

	// Return DREAM to staker
	if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(stakerAddr), stake.Amount); err != nil {
		return nil, err
	}

	// Update tranche dream_staked
	tranche.DreamStaked = tranche.DreamStaked.Sub(stake.Amount)

	// If tranche was BACKED and now drops below threshold, revert to STAKING
	if tranche.Status == types.TrancheStatus_TRANCHE_STATUS_BACKED &&
		tranche.DreamStaked.LT(tranche.StakeThreshold) {
		tranche.Status = types.TrancheStatus_TRANCHE_STATUS_STAKING
		tranche.BackedAt = 0
		tranche.RevealDeadline = 0
	}

	// Save updated contribution
	if err := k.Contribution.Set(ctx, contrib.Id, contrib); err != nil {
		return nil, err
	}

	// Remove stake and indexes
	if err := k.RevealStake.Remove(ctx, stake.Id); err != nil {
		return nil, err
	}
	trancheKey := TrancheKey(stake.ContributionId, stake.TrancheId)
	if err := k.StakesByTranche.Remove(ctx, collections.Join(trancheKey, stake.Id)); err != nil {
		return nil, err
	}
	if err := k.StakesByStaker.Remove(ctx, collections.Join(stake.Staker, stake.Id)); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"withdrawn",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stake.Id)),
			sdk.NewAttribute("staker", stake.Staker),
			sdk.NewAttribute("amount", stake.Amount.String()),
		),
	)

	return &types.MsgWithdrawResponse{}, nil
}
