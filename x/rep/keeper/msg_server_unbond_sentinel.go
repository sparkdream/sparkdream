package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UnbondSentinel(ctx context.Context, msg *types.MsgUnbondSentinel) (*types.MsgUnbondSentinelResponse, error) {
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	sa, err := k.SentinelActivity.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrSentinelNotFound, "not a registered sentinel")
	}

	unbondAmount, ok := math.NewIntFromString(msg.Amount)
	if !ok || unbondAmount.IsNegative() || unbondAmount.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid unbond amount")
	}

	currentBond := parseIntOrZero(sa.CurrentBond)
	if unbondAmount.GT(currentBond) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientSentinelBond,
			"cannot unbond %s, only %s bonded",
			unbondAmount.String(), currentBond.String())
	}

	committedBond := parseIntOrZero(sa.TotalCommittedBond)
	availableBond := currentBond.Sub(committedBond)
	if unbondAmount.GT(availableBond) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientSentinelBond,
			"only %s available to unbond (%s committed for pending actions)",
			availableBond.String(), committedBond.String())
	}

	// Release the DREAM back to the sentinel (undo the bond's LockDREAM).
	if err := k.UnlockDREAM(ctx, creatorAddr, unbondAmount); err != nil {
		return nil, errorsmod.Wrap(err, "failed to unlock DREAM bond")
	}

	newBond := currentBond.Sub(unbondAmount)
	sa.CurrentBond = newBond.String()

	minBond := math.NewInt(DefaultMinSentinelBondAmount)
	demotionThreshold := math.NewInt(DefaultSentinelDemotionThreshold)
	switch {
	case newBond.GTE(minBond):
		sa.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL
	case newBond.GTE(demotionThreshold):
		sa.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY
	default:
		sa.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED
		// Enter demotion cooldown only when crossing below the RECOVERY floor.
		if currentBond.GTE(demotionThreshold) {
			sa.DemotionCooldownUntil = sdkCtx.BlockTime().Unix() + DefaultSentinelDemotionCooldown
		}
	}

	if err := k.SentinelActivity.Set(ctx, msg.Creator, sa); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store sentinel activity")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"sentinel_unbonded",
			sdk.NewAttribute("sentinel", msg.Creator),
			sdk.NewAttribute("amount", unbondAmount.String()),
			sdk.NewAttribute("remaining_bond", sa.CurrentBond),
			sdk.NewAttribute("bond_status", sa.BondStatus.String()),
		),
	)

	return &types.MsgUnbondSentinelResponse{}, nil
}
