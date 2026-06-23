package keeper

import (
	"context"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
)

// eligibleSentinel returns the caller's FORUM_SENTINEL bond if it may take a
// moderation action, or a typed error explaining why not.
//
// NORMAL and RECOVERY are eligible outright. UNBONDING is eligible while the
// *staying* bond (current_bond - pending_unbond_amount) stays at or above
// min_sentinel_bond; the portion being withdrawn is treated as already gone.
// The action's own ReserveBond call (now pending-aware in x/rep) separately
// enforces that any reserved slash fits in the staying, uncommitted bond, so a
// slash taken during an unbond remains backed through its whole appeal window.
// DEMOTED (and any future ineligible status) is never eligible.
//
// There is deliberately no time / unbond-completion comparison: eligibility is
// a pure bond-quantity question. Safety comes from earmarked (committed) bond
// surviving the unbond — a slash reserved during an UNBONDING role stays locked
// and slashable through the action's whole appeal window — not from timing.
func (k Keeper) eligibleSentinel(ctx context.Context, addr string) (reptypes.BondedRole, error) {
	if k.repKeeper == nil {
		return reptypes.BondedRole{}, errorsmod.Wrap(types.ErrNotSentinel, "rep keeper not wired")
	}
	br, err := k.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr)
	if err != nil {
		return br, errorsmod.Wrap(types.ErrNotSentinel, "not a registered sentinel")
	}

	current, ok := math.NewIntFromString(br.CurrentBond)
	if !ok || br.CurrentBond == "" {
		return br, errorsmod.Wrapf(types.ErrInvalidAmount, "invalid bonded role current_bond: %q", br.CurrentBond)
	}

	switch br.BondStatus {
	case reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
		reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY:
		return br, nil

	case reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING:
		pending := math.ZeroInt()
		if br.PendingUnbondAmount != "" {
			p, pok := math.NewIntFromString(br.PendingUnbondAmount)
			if !pok {
				return br, errorsmod.Wrapf(types.ErrInvalidAmount,
					"invalid bonded role pending_unbond_amount: %q", br.PendingUnbondAmount)
			}
			pending = p
		}
		params, perr := k.Params.Get(ctx)
		if perr != nil {
			params = types.DefaultParams()
		}
		minBond, mok := math.NewIntFromString(params.MinSentinelBond)
		if !mok {
			return br, errorsmod.Wrapf(types.ErrInvalidAmount,
				"invalid min_sentinel_bond param: %q", params.MinSentinelBond)
		}
		if current.Sub(pending).LT(minBond) {
			return br, errorsmod.Wrap(types.ErrSentinelUnbonding,
				"staying bond below minimum during unbond")
		}
		return br, nil

	default: // DEMOTED and any future ineligible status
		return br, types.ErrSentinelDemoted
	}
}
