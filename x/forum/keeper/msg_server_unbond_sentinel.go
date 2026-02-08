package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UnbondSentinel(ctx context.Context, msg *types.MsgUnbondSentinel) (*types.MsgUnbondSentinelResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Load sentinel activity record
	sentinelActivity, err := k.SentinelActivity.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrSentinelNotFound, "not a registered sentinel")
	}

	// Check for pending hides - cannot unbond if there are pending appeals
	if sentinelActivity.PendingHideCount > 0 {
		return nil, errorsmod.Wrapf(types.ErrCannotUnbondPendingHides,
			"sentinel has %d pending hide(s) awaiting appeal resolution", sentinelActivity.PendingHideCount)
	}

	// Parse unbond amount
	unbondAmount, ok := math.NewIntFromString(msg.Amount)
	if !ok || unbondAmount.IsNegative() || unbondAmount.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid unbond amount")
	}

	// Check current bond
	currentBond, _ := math.NewIntFromString(sentinelActivity.CurrentBond)
	if sentinelActivity.CurrentBond == "" {
		currentBond = math.ZeroInt()
	}

	if unbondAmount.GT(currentBond) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientBond, "cannot unbond %s, only %s bonded",
			unbondAmount.String(), currentBond.String())
	}

	// Check available bond (current - committed)
	committedBond, _ := math.NewIntFromString(sentinelActivity.TotalCommittedBond)
	if sentinelActivity.TotalCommittedBond == "" {
		committedBond = math.ZeroInt()
	}
	availableBond := currentBond.Sub(committedBond)

	if unbondAmount.GT(availableBond) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientBond,
			"only %s available to unbond (%s committed for pending hides)",
			availableBond.String(), committedBond.String())
	}

	// Transfer DREAM from module back to user (stub - actual transfer via x/rep)
	if err := k.TransferDREAM(ctx, k.GetModuleAddress(), msg.Creator, unbondAmount); err != nil {
		return nil, errorsmod.Wrap(err, "failed to transfer DREAM back to user")
	}

	// Update bond
	newBond := currentBond.Sub(unbondAmount)
	sentinelActivity.CurrentBond = newBond.String()

	// Update bond status
	minBond := math.NewInt(1000)          // DefaultMinSentinelBond
	demotionThreshold := math.NewInt(500) // DefaultSentinelDemotionThreshold

	if newBond.GTE(minBond) {
		sentinelActivity.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL
	} else if newBond.GTE(demotionThreshold) {
		sentinelActivity.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY
	} else {
		sentinelActivity.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED
		// Set demotion cooldown if dropping below threshold
		if currentBond.GTE(demotionThreshold) {
			now := sdkCtx.BlockTime().Unix()
			sentinelActivity.DemotionCooldownUntil = now + types.DefaultSentinelDemotionCooldown
		}
	}

	// Store sentinel activity
	if err := k.SentinelActivity.Set(ctx, msg.Creator, sentinelActivity); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store sentinel activity")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"sentinel_unbonded",
			sdk.NewAttribute("sentinel", msg.Creator),
			sdk.NewAttribute("amount", fmt.Sprintf("%s", unbondAmount.String())),
			sdk.NewAttribute("remaining_bond", sentinelActivity.CurrentBond),
			sdk.NewAttribute("bond_status", sentinelActivity.BondStatus.String()),
		),
	)

	return &types.MsgUnbondSentinelResponse{}, nil
}
