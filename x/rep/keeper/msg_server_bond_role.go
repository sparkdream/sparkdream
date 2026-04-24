package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BondRole locks DREAM against (role_type, creator) and creates or updates
// the BondedRole record. Eligibility is enforced against the role's
// BondedRoleConfig (min_bond, min_rep_tier, min_trust_level). min_age_blocks
// is enforced by the owning module at action time, not here.
func (k msgServer) BondRole(ctx context.Context, msg *types.MsgBondRole) (*types.MsgBondRoleResponse, error) {
	if err := validateRoleType(msg.RoleType); err != nil {
		return nil, err
	}

	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	amount, ok := math.NewIntFromString(msg.Amount)
	if !ok || amount.IsNegative() || amount.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid bond amount")
	}

	cfg, err := k.GetBondedRoleConfig(ctx, msg.RoleType)
	if err != nil {
		return nil, err
	}

	// Rep-tier eligibility gate.
	if cfg.MinRepTier > 0 {
		tier, err := k.GetReputationTier(ctx, creatorAddr)
		if err != nil {
			return nil, err
		}
		if tier < cfg.MinRepTier {
			return nil, errorsmod.Wrapf(types.ErrInsufficientReputation,
				"tier %d required, have %d", cfg.MinRepTier, tier)
		}
	}

	// Trust-level eligibility gate.
	if cfg.MinTrustLevel != "" {
		required, okT := types.TrustLevel_value[cfg.MinTrustLevel]
		if !okT {
			return nil, errorsmod.Wrapf(types.ErrBondedRoleConfigMissing,
				"invalid min_trust_level %q", cfg.MinTrustLevel)
		}
		actual, err := k.GetTrustLevel(ctx, creatorAddr)
		if err != nil {
			return nil, err
		}
		if int32(actual) < required {
			return nil, errorsmod.Wrapf(types.ErrInsufficientReputation,
				"trust level %s required, have %s", cfg.MinTrustLevel, actual.String())
		}
	}

	// Minimum-bond gate. min_bond is the NORMAL-status floor; this handler
	// requires the aggregate current_bond after this deposit to reach that
	// floor on first bond. Top-ups below min_bond are allowed provided a
	// non-zero record already exists (so role-holders can rebuild in RECOVERY).
	minBond := parseIntOrZero(cfg.MinBond)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	key := bondedRoleKey(msg.RoleType, msg.Creator)
	br, err := k.BondedRoles.Get(ctx, key)
	existing := err == nil
	if !existing {
		br = types.BondedRole{
			Address:            msg.Creator,
			RoleType:           msg.RoleType,
			BondStatus:         types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
			CurrentBond:        "0",
			TotalCommittedBond: "0",
			CumulativeRewards:  "0",
			RegisteredAt:       sdkCtx.BlockHeight(),
		}
	}

	// Enforce demotion cooldown for re-bonding DEMOTED roles.
	if br.DemotionCooldownUntil > now {
		return nil, errorsmod.Wrapf(types.ErrDemotionCooldown,
			"cannot bond until %d", br.DemotionCooldownUntil)
	}

	currentBond := parseIntOrZero(br.CurrentBond)
	newBond := currentBond.Add(amount)

	// First bond must land the record at or above min_bond (otherwise the
	// role would start in RECOVERY / DEMOTED from the outset, which is
	// likely not what the user intended).
	if !existing && newBond.LT(minBond) {
		return nil, errorsmod.Wrapf(types.ErrBondAmountTooSmall,
			"minimum bond is %s DREAM", minBond.String())
	}

	// Lock DREAM from the bonder (author-bond pattern: locked balance cannot
	// decay and is slashable via UnlockDREAM + BurnDREAM).
	if err := k.LockDREAM(ctx, creatorAddr, amount); err != nil {
		return nil, errorsmod.Wrap(err, "failed to lock DREAM bond")
	}

	br.CurrentBond = newBond.String()
	br.BondStatus = k.computeBondStatus(ctx, msg.RoleType, newBond)

	if err := k.BondedRoles.Set(ctx, key, br); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store bonded role")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bonded_role_bonded",
			sdk.NewAttribute("role_type", msg.RoleType.String()),
			sdk.NewAttribute("address", msg.Creator),
			sdk.NewAttribute("amount", msg.Amount),
			sdk.NewAttribute("total_bond", br.CurrentBond),
			sdk.NewAttribute("bond_status", br.BondStatus.String()),
		),
	)

	return &types.MsgBondRoleResponse{}, nil
}
