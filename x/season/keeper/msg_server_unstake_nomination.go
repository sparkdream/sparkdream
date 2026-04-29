package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UnstakeNomination removes a DREAM stake from a nomination.
func (k msgServer) UnstakeNomination(ctx context.Context, msg *types.MsgUnstakeNomination) (*types.MsgUnstakeNominationResponse, error) {
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

	// 3. Find the staker's NominationStake record
	stakeKey := fmt.Sprintf("%d/%s", msg.NominationId, msg.Creator)
	stake, err := k.NominationStake.Get(ctx, stakeKey)
	if err != nil {
		// 4. If not found, return ErrNominationStakeNotFound
		return nil, types.ErrNominationStakeNotFound
	}

	// 5. Convert amount to integer and unlock DREAM
	amountInt := stake.Amount.TruncateInt()
	if !amountInt.IsZero() {
		if !amountInt.BigInt().IsUint64() {
			return nil, fmt.Errorf("stake amount overflows uint64")
		}
		if err := k.UnlockDREAM(ctx, msg.Creator, amountInt.Uint64()); err != nil {
			return nil, errorsmod.Wrap(types.ErrDREAMOperationFailed, "failed to unlock DREAM for nomination unstake")
		}
	}

	// 6. Remove the NominationStake record
	if err := k.NominationStake.Remove(ctx, stakeKey); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove nomination stake")
	}

	// 7. Update nomination's total_staked by subtracting amount
	nomination.TotalStaked = nomination.TotalStaked.Sub(stake.Amount)

	// 8. Recalculate conviction
	conviction, err := k.CalculateNominationConviction(ctx, nomination)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to calculate conviction")
	}
	nomination.Conviction = conviction

	// 9. Save updated nomination
	if err := k.Nomination.Set(ctx, msg.NominationId, nomination); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update nomination")
	}

	// 10. Emit "nomination_unstaked" event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"nomination_unstaked",
			sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", msg.NominationId)),
			sdk.NewAttribute("staker", msg.Creator),
			sdk.NewAttribute("amount", stake.Amount.String()),
			sdk.NewAttribute("total_staked", nomination.TotalStaked.String()),
			sdk.NewAttribute("conviction", nomination.Conviction.String()),
		),
	)

	return &types.MsgUnstakeNominationResponse{}, nil
}
