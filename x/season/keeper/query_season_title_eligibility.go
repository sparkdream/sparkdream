package keeper

import (
	"context"
	"errors"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListSeasonTitleEligibility(ctx context.Context, req *types.QueryAllSeasonTitleEligibilityRequest) (*types.QueryAllSeasonTitleEligibilityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	seasonTitleEligibilitys, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.SeasonTitleEligibility,
		req.Pagination,
		func(_ uint64, value types.SeasonTitleEligibility) (types.SeasonTitleEligibility, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllSeasonTitleEligibilityResponse{SeasonTitleEligibility: seasonTitleEligibilitys, Pagination: pageRes}, nil
}

func (q queryServer) GetSeasonTitleEligibility(ctx context.Context, req *types.QueryGetSeasonTitleEligibilityRequest) (*types.QueryGetSeasonTitleEligibilityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.SeasonTitleEligibility.Get(ctx, req.TitleSeason)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetSeasonTitleEligibilityResponse{SeasonTitleEligibility: val}, nil
}
