package keeper

import (
	"context"
	"fmt"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// StakeNomination stakes DREAM on a nomination.
func (k msgServer) StakeNomination(ctx context.Context, msg *types.MsgStakeNomination) (*types.MsgStakeNominationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check maintenance mode
	if k.IsInMaintenanceMode(ctx) {
		return nil, types.ErrMaintenanceMode
	}

	// 1. Get current season, validate status is SEASON_STATUS_NOMINATION
	season, err := k.Season.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrSeasonNotActive, "no active season found")
	}
	if season.Status != types.SeasonStatus_SEASON_STATUS_NOMINATION {
		return nil, types.ErrSeasonNotInNominationPhase
	}

	// 2. Get the nomination by ID, validate it exists
	nomination, err := k.Nomination.Get(ctx, msg.NominationId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrNominationNotFound, "nomination %d not found", msg.NominationId)
	}

	// 3. Check staker is a member with sufficient trust level
	if k.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrNotMember, "reputation module not available")
	}
	stakerAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	if !k.repKeeper.IsMember(ctx, stakerAddr) {
		return nil, types.ErrNotMember
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	trustLevel, err := k.repKeeper.GetTrustLevel(ctx, stakerAddr)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get trust level")
	}
	if trustLevel < reptypes.TrustLevel(params.NominationStakeMinTrustLevel) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
			"trust level %d < required %d", trustLevel, params.NominationStakeMinTrustLevel)
	}

	// 4. Parse amount string as math.LegacyDec, validate >= NominationMinStake
	amount, err := math.LegacyNewDecFromStr(msg.Amount)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "invalid stake amount: %s", msg.Amount)
	}
	if amount.LT(params.NominationMinStake) {
		return nil, errorsmod.Wrapf(types.ErrStakeAmountTooLow,
			"amount %s < minimum %s", amount.String(), params.NominationMinStake.String())
	}

	// 5. Check staker hasn't already staked on this nomination
	stakeKey := fmt.Sprintf("%d/%s", msg.NominationId, msg.Creator)
	_, err = k.NominationStake.Get(ctx, stakeKey)
	if err == nil {
		return nil, types.ErrNominationStakeExists
	}

	// 6. Check staker is not the nominator (can't self-stake)
	if msg.Creator == nomination.Nominator {
		return nil, errorsmod.Wrap(types.ErrNominationStakeExists, "cannot stake on own nomination")
	}

	// 7. Convert amount to integer and lock DREAM
	amountInt := amount.TruncateInt()
	if amountInt.IsZero() {
		return nil, errorsmod.Wrap(types.ErrStakeAmountTooLow, "truncated amount is zero")
	}
	if err := k.LockDREAM(ctx, msg.Creator, amountInt.Uint64()); err != nil {
		return nil, errorsmod.Wrap(types.ErrDREAMOperationFailed, "failed to lock DREAM for nomination stake")
	}

	// 8. Create NominationStake record and save
	stake := types.NominationStake{
		NominationId:  msg.NominationId,
		Staker:        msg.Creator,
		Amount:        amount,
		StakedAtBlock: sdkCtx.BlockHeight(),
	}
	if err := k.NominationStake.Set(ctx, stakeKey, stake); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save nomination stake")
	}

	// 9. Update nomination's total_staked by adding amount
	nomination.TotalStaked = nomination.TotalStaked.Add(amount)

	// 10. Recalculate conviction
	conviction, err := k.CalculateNominationConviction(ctx, nomination)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to calculate conviction")
	}
	nomination.Conviction = conviction

	// 11. Save updated nomination
	if err := k.Nomination.Set(ctx, msg.NominationId, nomination); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update nomination")
	}

	// 12. Emit "nomination_staked" event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"nomination_staked",
			sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", msg.NominationId)),
			sdk.NewAttribute("staker", msg.Creator),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("total_staked", nomination.TotalStaked.String()),
			sdk.NewAttribute("conviction", nomination.Conviction.String()),
		),
	)

	return &types.MsgStakeNominationResponse{}, nil
}
