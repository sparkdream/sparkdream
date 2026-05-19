package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListIdentityLinks(ctx context.Context, req *types.QueryListIdentityLinksRequest) (*types.QueryListIdentityLinksResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	links, pageRes, err := query.CollectionPaginate(
		ctx, q.k.IdentityLinks, req.Pagination,
		func(_ collections.Pair[string, string], value types.IdentityLink) (types.IdentityLink, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListIdentityLinksResponse{
		Links:      links,
		Pagination: pageRes,
	}, nil
}
