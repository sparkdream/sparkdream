package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UnbondRole initiates withdrawal of a portion of the caller's bond for the
// given role_type. If the role's BondedRoleConfig.UnbondCooldown is positive,
// the DREAM stays locked and slashable, the BondedRole.PendingUnbondAmount and
// UnbondCompletionTime are set, and bond_status flips to UNBONDING — the
// owning module's action gates must refuse on this status to contain new
// liability while the bond drains. The EndBlocker matures pending unbonds via
// MatureUnbonds: at that point the DREAM is unlocked, pending fields cleared,
// status flipped to DEMOTED, and demotion_cooldown starts gating re-bonding.
//
// When UnbondCooldown is zero, the behavior is the legacy immediate withdrawal:
// DREAM is unlocked immediately and status is recomputed from the new bond.
//
// One active unbond per role at a time: calling UnbondRole again on a role
// that is already UNBONDING is rejected. The committed-bond invariant
// (current_bond - total_committed_bond >= amount) is enforced so outstanding
// liability stays fully collateralized through the cooldown.
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

	// Reject overlapping unbonds — one in-flight at a time keeps the state
	// machine and the slash-accounting invariants simple.
	if br.BondStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest,
			"role is already UNBONDING; wait for completion or top-up via a fresh bond cycle")
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

	// Load config to determine cooldown + thresholds. Graceful no-config
	// fallback preserves the legacy immediate-withdrawal behavior so test
	// harnesses without a config seed still pass.
	cfg, cfgErr := k.GetBondedRoleConfig(ctx, msg.RoleType)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Path A: cooldown configured — queue the unbond, keep DREAM locked.
	if cfgErr == nil && cfg.UnbondCooldown > 0 {
		br.PendingUnbondAmount = amount.String()
		br.UnbondCompletionTime = now + cfg.UnbondCooldown
		br.BondStatus = types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING

		if err := k.BondedRoles.Set(ctx, key, br); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store bonded role")
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"bonded_role_unbond_initiated",
				sdk.NewAttribute("role_type", msg.RoleType.String()),
				sdk.NewAttribute("address", msg.Creator),
				sdk.NewAttribute("amount", amount.String()),
				sdk.NewAttribute("completion_time", fmt.Sprintf("%d", br.UnbondCompletionTime)),
				sdk.NewAttribute("bond_status", br.BondStatus.String()),
			),
		)
		return &types.MsgUnbondRoleResponse{}, nil
	}

	// Path B: no cooldown — legacy immediate unlock.
	if err := k.UnlockDREAM(ctx, creatorAddr, amount); err != nil {
		return nil, errorsmod.Wrap(err, "failed to unlock DREAM bond")
	}

	newBond := currentBond.Sub(amount)
	br.CurrentBond = newBond.String()

	prevStatus := br.BondStatus
	newStatus := k.computeBondStatus(ctx, msg.RoleType, newBond)

	if newStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED && committed.IsPositive() {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "cannot unbond below required threshold while bond is committed")
	}
	br.BondStatus = newStatus

	if cfgErr == nil &&
		br.BondStatus == types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED &&
		prevStatus != types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED {
		br.DemotionCooldownUntil = now + cfg.DemotionCooldown
	}

	if err := k.BondedRoles.Set(ctx, key, br); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store bonded role")
	}

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
