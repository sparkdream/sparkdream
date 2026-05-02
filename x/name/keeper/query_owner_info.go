package keeper

import (
	"context"
	"errors"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetOwnerInfo returns the OwnerInfo for an address. Works for any address,
// including ones that never registered a handle: when no record exists, the
// response echoes the address with all other fields empty/zero so a UI can
// always render something instead of branching on NotFound.
func (q queryServer) GetOwnerInfo(ctx context.Context, req *types.QueryGetOwnerInfoRequest) (*types.QueryGetOwnerInfoResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}

	info, err := q.k.Owners.Get(ctx, req.Address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return &types.QueryGetOwnerInfoResponse{OwnerInfo: types.OwnerInfo{Address: req.Address}}, nil
		}
		return nil, status.Error(codes.Internal, "internal error")
	}
	return &types.QueryGetOwnerInfoResponse{OwnerInfo: info}, nil
}
