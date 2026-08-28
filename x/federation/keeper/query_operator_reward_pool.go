package keeper

import (
	"context"

	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/federation/types"
)

// OperatorRewardPool reports the bridge-operator SPARK pool's balance, cap and
// today's draw against the daily allowance.
//
// Exists because "why was I not paid this epoch" otherwise has no on-chain
// answer: an operator can be ineligible, or eligible against an empty pool,
// and the two look identical from the outside.
func (q queryServer) OperatorRewardPool(ctx context.Context, req *types.QueryOperatorRewardPoolRequest) (*types.QueryOperatorRewardPoolResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to load params")
	}

	balance := q.k.GetOperatorRewardPool(ctx)
	poolCap := params.MaxOperatorRewardPool
	if poolCap.IsNil() {
		poolCap = math.ZeroInt()
	}
	headroom := poolCap.Sub(balance)
	if headroom.IsNegative() {
		headroom = math.ZeroInt()
	}

	share := params.OperatorRewardInflationShare
	if share.IsNil() {
		share = math.LegacyZeroDec()
	}

	day := utcDayOf(sdkBlockTime(ctx))
	return &types.QueryOperatorRewardPoolResponse{
		Address:         OperatorRewardPoolAddress().String(),
		Balance:         balance,
		Cap:             poolCap,
		Headroom:        headroom,
		FundedToday:     q.k.GetOperatorRewardDayFunding(ctx, day),
		DailyFundingCap: q.k.OperatorRewardDailyAllowance(ctx, params),
		InflationShare:  share,
	}, nil
}
