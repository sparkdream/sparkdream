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

func (q queryServer) ListDisplayNameReportStake(ctx context.Context, req *types.QueryAllDisplayNameReportStakeRequest) (*types.QueryAllDisplayNameReportStakeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	displayNameReportStakes, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.DisplayNameReportStake,
		req.Pagination,
		func(_ string, value types.DisplayNameReportStake) (types.DisplayNameReportStake, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllDisplayNameReportStakeResponse{DisplayNameReportStake: displayNameReportStakes, Pagination: pageRes}, nil
}

func (q queryServer) GetDisplayNameReportStake(ctx context.Context, req *types.QueryGetDisplayNameReportStakeRequest) (*types.QueryGetDisplayNameReportStakeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.DisplayNameReportStake.Get(ctx, req.ChallengeId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetDisplayNameReportStakeResponse{DisplayNameReportStake: val}, nil
}
