package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/commons/types"
)

// TreasuryStake reports the SPARK group treasuries have staked together and
// the room left under the treasury stake cap (treasury_staking.go).
func (q queryServer) TreasuryStake(ctx context.Context, req *types.QueryTreasuryStakeRequest) (*types.QueryTreasuryStakeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	staked, bonded, headroom, err := q.k.TreasuryStake(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryTreasuryStakeResponse{
		TreasuryStaked: staked,
		TotalBonded:    bonded,
		CapBps:         TreasuryStakeCapBps,
		Headroom:       headroom,
	}, nil
}
