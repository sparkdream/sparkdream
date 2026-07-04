package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// eligibleSentinel returns the caller's FORUM_SENTINEL bond if it may take a
// moderation action, or a typed forum error explaining why not.
//
// The eligibility logic itself (NORMAL/RECOVERY outright; UNBONDING while
// the *staying* bond covers the role's configured min_bond; DEMOTED never)
// lives in x/rep as Keeper.EligibleForRole — hoisted there from this file so
// every module consuming the shared sentinel role (forum + collect) enforces
// the same gate. This wrapper only maps the rep-typed errors onto forum's
// error surface. The min-bond threshold comes from rep's BondedRoleConfig,
// which forum writes through on InitGenesis and every sentinel-config
// operational-params change, so it always mirrors params.MinSentinelBond.
func (k Keeper) eligibleSentinel(ctx context.Context, addr string) (reptypes.BondedRole, error) {
	if k.repKeeper == nil {
		return reptypes.BondedRole{}, errorsmod.Wrap(types.ErrNotSentinel, "rep keeper not wired")
	}

	br, err := k.repKeeper.EligibleForRole(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, addr)
	if err == nil {
		return br, nil
	}

	switch {
	case errors.Is(err, reptypes.ErrBondedRoleNotFound):
		return br, errorsmod.Wrap(types.ErrNotSentinel, "not a registered sentinel")
	case errors.Is(err, reptypes.ErrRoleUnbondingBelowMin):
		return br, errorsmod.Wrap(types.ErrSentinelUnbonding,
			"staying bond below minimum during unbond")
	case errors.Is(err, reptypes.ErrRoleDemoted):
		return br, types.ErrSentinelDemoted
	default:
		return br, err
	}
}
