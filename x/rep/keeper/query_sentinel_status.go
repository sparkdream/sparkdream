package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/types"
)

func (q queryServer) SentinelStatus(ctx context.Context, req *types.QuerySentinelStatusRequest) (*types.QuerySentinelStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address required")
	}
	sa, err := q.k.SentinelActivity.Get(ctx, req.Address)
	if err != nil {
		return nil, status.Error(codes.NotFound, "sentinel not found")
	}
	return &types.QuerySentinelStatusResponse{
		Address:     sa.Address,
		BondStatus:  uint64(sa.BondStatus),
		CurrentBond: sa.CurrentBond,
	}, nil
}
