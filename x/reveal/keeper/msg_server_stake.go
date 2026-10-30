package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/reveal/types"
)

func (k msgServer) Stake(ctx context.Context, msg *types.MsgStake) (*types.MsgStakeResponse, error) {
	stakerAddr, err := k.addressCodec.StringToBytes(msg.Staker)
	if err != nil {
		return nil, types.ErrNotMember.Wrapf("invalid staker address: %s", err)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Verify staker is an active member at PROVISIONAL or above (spec)
	if !k.repKeeper.IsActiveMember(ctx, sdk.AccAddress(stakerAddr)) {
		return nil, types.ErrNotMember
	}
	if level, err := k.repKeeper.GetTrustLevel(ctx, sdk.AccAddress(stakerAddr)); err != nil || level < reptypes.TrustLevel_TRUST_LEVEL_PROVISIONAL {
		return nil, types.ErrInsufficientTrustLevel.Wrapf("staking requires %s", reptypes.TrustLevel_TRUST_LEVEL_PROVISIONAL)
	}

	// Get contribution
	contrib, err := k.Contribution.Get(ctx, msg.ContributionId)
	if err != nil {
		return nil, types.ErrContributionNotFound.Wrapf("contribution %d", msg.ContributionId)
	}

	// Must be IN_PROGRESS
	if contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS {
		return nil, types.ErrNotInProgress
	}

	// Self-stake prevention: contributor cannot stake on own contribution
	if k.sameMember(ctx, msg.Staker, contrib.Contributor) {
		return nil, types.ErrSelfStake
	}

	// Get tranche
	tranche, err := GetTranche(&contrib, msg.TrancheId)
	if err != nil {
		return nil, err
	}

	// Tranche must be in STAKING status
	if tranche.Status != types.TrancheStatus_TRANCHE_STATUS_STAKING {
		return nil, types.ErrTrancheNotStaking
	}

	// Check minimum stake amount. A stake smaller than the minimum is
	// allowed only when it fills the tranche exactly.
	remaining := tranche.StakeThreshold.Sub(tranche.DreamStaked)
	if msg.Amount.LT(params.MinStakeAmount) && !msg.Amount.Equal(remaining) {
		return nil, types.ErrStakeAmountTooLow.Wrapf("min %s, got %s", params.MinStakeAmount, msg.Amount)
	}
	// Never leave a remainder no stake could fill.
	if rest := remaining.Sub(msg.Amount); rest.IsPositive() && rest.LT(params.MinStakeAmount) {
		return nil, types.ErrStakeLeavesUnfillableRest.Wrapf("remainder %s, min_stake_amount %s", rest, params.MinStakeAmount)
	}

	// Check overstaking: total staked + amount must not exceed threshold
	newTotal := tranche.DreamStaked.Add(msg.Amount)
	if newTotal.GT(tranche.StakeThreshold) {
		return nil, types.ErrStakeExceedsThreshold.Wrapf(
			"threshold %s, already staked %s, attempting %s",
			tranche.StakeThreshold, tranche.DreamStaked, msg.Amount,
		)
	}

	// Lock DREAM from staker
	if err := k.repKeeper.LockDREAM(ctx, sdk.AccAddress(stakerAddr), msg.Amount); err != nil {
		return nil, err
	}

	// Allocate stake ID
	stakeID, err := k.StakeSeq.Next(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentEpoch := sdkCtx.BlockHeight()

	stake := types.RevealStake{
		Id:             stakeID,
		Staker:         msg.Staker,
		ContributionId: msg.ContributionId,
		TrancheId:      msg.TrancheId,
		Amount:         msg.Amount,
		StakedAt:       currentEpoch,
	}

	// Save stake and indexes
	if err := k.RevealStake.Set(ctx, stakeID, stake); err != nil {
		return nil, err
	}
	trancheKey := TrancheKey(msg.ContributionId, msg.TrancheId)
	if err := k.StakesByTranche.Set(ctx, collections.Join(trancheKey, stakeID)); err != nil {
		return nil, err
	}
	if err := k.StakesByStaker.Set(ctx, collections.Join(msg.Staker, stakeID)); err != nil {
		return nil, err
	}

	// Update tranche dream_staked
	tranche.DreamStaked = newTotal

	// Check if tranche is now BACKED
	if tranche.DreamStaked.GTE(tranche.StakeThreshold) {
		tranche.Status = types.TrancheStatus_TRANCHE_STATUS_BACKED
		tranche.BackedAt = currentEpoch

		params, err := k.Params.Get(ctx)
		if err != nil {
			return nil, err
		}
		tranche.RevealDeadline = currentEpoch + params.RevealDeadlineEpochs

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"tranche_backed",
				sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
				sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", msg.TrancheId)),
				sdk.NewAttribute("dream_staked", tranche.DreamStaked.String()),
			),
		)
	}

	// REVEAL-3 fix: Single write after all modifications are complete.
	// Previously there were two k.Contribution.Set() calls with potentially
	// inconsistent state due to pointer aliasing (tranche is a pointer into contrib.Tranches).
	if err := k.saveContribution(ctx, &contrib); err != nil {
		return nil, err
	}

	// Emit stake event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"staked",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stakeID)),
			sdk.NewAttribute("staker", msg.Staker),
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", msg.ContributionId)),
			sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", msg.TrancheId)),
			sdk.NewAttribute("amount", msg.Amount.String()),
		),
	)

	return &types.MsgStakeResponse{StakeId: stakeID}, nil
}
