package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"

	"sparkdream/x/reveal/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Contributions(ctx context.Context, req *types.QueryContributionsRequest) (*types.QueryContributionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	contributions, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Contribution,
		req.Pagination,
		func(key uint64, value types.Contribution) (types.Contribution, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryContributionsResponse{
		Contributions: contributions,
		Pagination:    pageRes,
	}, nil
}
