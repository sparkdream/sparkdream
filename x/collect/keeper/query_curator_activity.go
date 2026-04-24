package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/collect/types"
)

// CuratorActivity returns the collect-specific per-curator counter record for
// the given address. Returns a zero-valued record with the address populated
// when no counters have been recorded yet (common before the curator has
// submitted any reviews).
func (q queryServer) CuratorActivity(ctx context.Context, req *types.QueryCuratorActivityRequest) (*types.QueryCuratorActivityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address required")
	}
	act, err := q.k.CuratorActivity.Get(ctx, req.Address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return &types.QueryCuratorActivityResponse{
				Activity: types.CuratorActivity{Address: req.Address},
			}, nil
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryCuratorActivityResponse{Activity: act}, nil
}
