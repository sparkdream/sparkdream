package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) SentinelStatus(ctx context.Context, req *types.QuerySentinelStatusRequest) (*types.QuerySentinelStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address required")
	}

	// Get sentinel activity
	sentinelActivity, err := q.k.SentinelActivity.Get(ctx, req.Address)
	if err != nil {
		return nil, status.Error(codes.NotFound, "sentinel not found")
	}

	return &types.QuerySentinelStatusResponse{
		Address:     sentinelActivity.Address,
		BondStatus:  uint64(sentinelActivity.BondStatus),
		CurrentBond: sentinelActivity.CurrentBond,
	}, nil
}
