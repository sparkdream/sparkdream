package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RoleRewardPools exposes what automatic funding is doing. The pools are
// derived sub-addresses with no other read surface, so without this a committee
// tuning role_reward_daily_funding would be adjusting a dial it cannot see.
func (q queryServer) RoleRewardPools(ctx context.Context, req *types.QueryRoleRewardPoolsRequest) (*types.QueryRoleRewardPoolsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	denom := q.k.BondDenom(ctx)
	pools := q.k.fundedRolePools(params)
	out := make([]types.RoleRewardPoolStatus, 0, len(pools))
	for _, p := range pools {
		balance := q.k.bankKeeper.GetBalance(ctx, p.addr, denom).Amount
		cap := p.maxPool
		if cap.IsNil() {
			cap = math.ZeroInt()
		}
		headroom := cap.Sub(balance)
		if headroom.IsNegative() {
			headroom = math.ZeroInt()
		}
		out = append(out, types.RoleRewardPoolStatus{
			Role:     p.name,
			Address:  p.addr.String(),
			Balance:  balance,
			Cap:      cap,
			Headroom: headroom,
		})
	}

	// Same bucketing as the funder, or this reports a day the ledger never charged.
	day := utcDayOf(sdk.UnwrapSDKContext(ctx).BlockTime())
	return &types.QueryRoleRewardPoolsResponse{
		Pools:           out,
		FundedToday:     q.k.GetRoleRewardDayFunding(ctx, day),
		DailyFundingCap: q.k.roleRewardDailyAllowance(ctx, params),
		InflationShare:  params.RoleRewardInflationShare,
	}, nil
}
