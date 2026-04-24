package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/federation/types"
)

// VerifierActivity returns the federation-specific per-verifier counter record
// for the given address. Returns a zero-valued record with the address
// populated when the verifier has no counters yet (common before they've
// submitted any verifications).
func (q queryServer) VerifierActivity(ctx context.Context, req *types.QueryVerifierActivityRequest) (*types.QueryVerifierActivityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address required")
	}
	activity, err := q.k.VerifierActivity.Get(ctx, req.Address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return &types.QueryVerifierActivityResponse{
				Activity: types.VerifierActivity{Address: req.Address},
			}, nil
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryVerifierActivityResponse{Activity: activity}, nil
}
