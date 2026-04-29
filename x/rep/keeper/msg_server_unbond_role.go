package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UnbondRole withdraws a portion of the caller's bond for the given role_type,
// subject to committed-bond constraints. Transitions bond_status per the
// role's config on new current_bond, and enters demotion cooldown when
// crossing below the demotion_threshold.
func (k msgServer) UnbondRole(ctx context.Context, msg *types.MsgUnbondRole) (*types.MsgUnbondRoleResponse, error) {
	if err := validateRoleType(msg.RoleType); err != nil {
		return nil, err
	}

	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	amount, ok := math.NewIntFromString(msg.Amount)
	if !ok || amount.IsNegative() || amount.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid unbond amount")
	}

	key := bondedRoleKey(msg.RoleType, msg.Creator)
	br, err := k.BondedRoles.Get(ctx, key)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBondedRoleNotFound,
			"%s:%s", msg.RoleType.String(), msg.Creator)
	}

	currentBond, err := parseIntOrZero(br.CurrentBond)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid current_bond in bonded role record")
	}
	if amount.GT(currentBond) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientBond,
			"cannot unbond %s, only %s bonded", amount.String(), currentBond.String())
	}

	committed, err := parseIntOrZero(br.TotalCommittedBond)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid total_committed_bond in bonded role record")
	}
	available := currentBond.Sub(committed)
	if amount.GT(available) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientBond,
			"only %s available to unbond (%s committed for pending actions)",
			available.String(), committed.String())
	}

	if err := k.UnlockDREAM(ctx, creatorAddr, amount); err != nil {
		return nil, errorsmod.Wrap(err, "failed to unlock DREAM bond")
	}

	newBond := currentBond.Sub(amount)
	br.CurrentBond = newBond.String()

	// Load config for cooldown trigger; graceful no-config fallback.
	cfg, cfgErr := k.GetBondedRoleConfig(ctx, msg.RoleType)

	prevStatus := br.BondStatus
	newStatus := k.computeBondStatus(ctx, msg.RoleType, newBond)

	// Reject unbonds that would drop the role below the recovery threshold while actions
	// are still committed — outstanding liability must remain fully collateralized.
	if newStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED && committed.IsPositive() {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "cannot unbond below required threshold while bond is committed")
	}
	br.BondStatus = newStatus

	// Enter demotion cooldown only when crossing below the RECOVERY floor.
	if cfgErr == nil &&
		br.BondStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED &&
		prevStatus != types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		br.DemotionCooldownUntil = sdkCtx.BlockTime().Unix() + cfg.DemotionCooldown
	}

	if err := k.BondedRoles.Set(ctx, key, br); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store bonded role")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bonded_role_unbonded",
			sdk.NewAttribute("role_type", msg.RoleType.String()),
			sdk.NewAttribute("address", msg.Creator),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("remaining_bond", br.CurrentBond),
			sdk.NewAttribute("bond_status", br.BondStatus.String()),
		),
	)

	return &types.MsgUnbondRoleResponse{}, nil
}
