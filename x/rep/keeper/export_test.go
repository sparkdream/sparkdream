package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
)

// RoleRewardDailyAllowanceForTest exposes the unexported allowance computation
// so the share arithmetic can be asserted directly rather than inferred from a
// draw.
func (k Keeper) RoleRewardDailyAllowanceForTest(ctx context.Context, params types.Params) math.Int {
	return k.roleRewardDailyAllowance(ctx, params)
}

// SetSeasonalPoolRemainingForTest exposes the unexported remaining-budget
// writer so pool-budget tests can stage a specific unspent balance (carryover
// semantics, drain isolation) without driving 150 epochs of distribution.
func (k Keeper) SetSeasonalPoolRemainingForTest(ctx context.Context, val math.Int) error {
	return k.setSeasonalPoolRemaining(ctx, val)
}

// SetSeasonalPoolStartEpochForTest exposes the unexported drain-schedule anchor
// writer so the anchored slice arithmetic can be tested directly.
func (k Keeper) SetSeasonalPoolStartEpochForTest(ctx context.Context, epoch uint64) error {
	return k.SeasonalPoolStartEpoch.Set(ctx, epoch)
}

// SetBondedRoleForTest exposes the bonded-role collection's composite key so a
// test can stage an exact role record — a bond above min_bond sitting on an
// unexpired demotion cooldown, for instance, which is reachable in production
// only through a slash sequence.
func (k Keeper) SetBondedRoleForTest(ctx context.Context, br types.BondedRole) error {
	return k.BondedRoles.Set(ctx, bondedRoleKey(br.RoleType, br.Address), br)
}
