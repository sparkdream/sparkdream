package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Aliases returns the canonical address of a member with every address the
// identity ever used (addresses) and the ones inside the dedup horizon
// (recent); both lists start with the canonical address.
func (q queryServer) Aliases(ctx context.Context, req *types.QueryAliasesRequest) (*types.QueryAliasesResponse, error) {
	if req == nil || req.Member == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	return &types.QueryAliasesResponse{
		Canonical: q.k.Canonical(ctx, req.Member),
		Addresses: q.k.AllAddressesOf(ctx, req.Member),
		Recent:    q.k.RecentAddressesOf(ctx, req.Member),
	}, nil
}
