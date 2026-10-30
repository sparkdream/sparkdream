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
	if !k.canActAs(ctx, msg.Staker, stake.Staker) {
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
	case types.TrancheStatus_TRANCHE_STATUS_STAKING:
		// Allowed
	case types.TrancheStatus_TRANCHE_STATUS_BACKED:
		// Stakes on a BACKED tranche stay committed until the contributor
		// reveals (or misses the reveal deadline). Stake() never lets a
		// tranche exceed its threshold, so any withdrawal here would drop it
		// below threshold and re-arm a stake deadline that has usually
		// already passed: the next EndBlock would cancel the contribution as
		// a "stake timeout", returning the bond instead of slashing it and
		// burning every earlier tranche's holdback (REV-02).
		if tranche.DreamStaked.Sub(stake.Amount).LT(tranche.StakeThreshold) {
			return nil, types.ErrWithdrawalNotAllowed.Wrap("tranche is BACKED: stakes stay committed until reveal or the reveal deadline")
		}
	case types.TrancheStatus_TRANCHE_STATUS_REVEALED,
		types.TrancheStatus_TRANCHE_STATUS_DISPUTED:
		return nil, types.ErrWithdrawalNotAllowed.Wrapf("tranche is in %s status", tranche.Status)
	default:
		// VERIFIED, CANCELLED, FAILED, LOCKED - stakes are auto-returned
		return nil, types.ErrWithdrawalNotAllowed.Wrapf("tranche is in %s status", tranche.Status)
	}

	// Return DREAM to staker (a zeroed staker's lock is already gone)
	if _, err := k.unlockOrGone(ctx, sdk.AccAddress(stakerAddr), stake.Amount); err != nil {
		return nil, err
	}

	// Update tranche dream_staked
	tranche.DreamStaked = tranche.DreamStaked.Sub(stake.Amount)

	// Save updated contribution
	if err := k.saveContribution(ctx, &contrib); err != nil {
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
