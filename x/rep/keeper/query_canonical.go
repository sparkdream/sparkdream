package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Canonical resolves a (possibly tombstoned) address to the current address
// of its identity.
func (q queryServer) Canonical(ctx context.Context, req *types.QueryCanonicalRequest) (*types.QueryCanonicalResponse, error) {
	if req == nil || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	resp := &types.QueryCanonicalResponse{Canonical: req.Address}
	if t, found := q.k.GetTombstone(ctx, req.Address); found {
		resp.Canonical = t.Canonical
		resp.Tombstoned = true
		resp.Path = t.Path
	}
	return resp, nil
}
