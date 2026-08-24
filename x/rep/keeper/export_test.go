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
