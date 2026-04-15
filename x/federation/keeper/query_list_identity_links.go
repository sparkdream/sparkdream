package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListIdentityLinks(ctx context.Context, req *types.QueryListIdentityLinksRequest) (*types.QueryListIdentityLinksResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var links []types.IdentityLink
	err := q.k.IdentityLinks.Walk(ctx, nil, func(key collections.Pair[string, string], value types.IdentityLink) (bool, error) {
		links = append(links, value)
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListIdentityLinksResponse{
		Links: links,
	}, nil
}
