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

func (q queryServer) ListDisplayNameAppealStake(ctx context.Context, req *types.QueryAllDisplayNameAppealStakeRequest) (*types.QueryAllDisplayNameAppealStakeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	displayNameAppealStakes, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.DisplayNameAppealStake,
		req.Pagination,
		func(_ string, value types.DisplayNameAppealStake) (types.DisplayNameAppealStake, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllDisplayNameAppealStakeResponse{DisplayNameAppealStake: displayNameAppealStakes, Pagination: pageRes}, nil
}

func (q queryServer) GetDisplayNameAppealStake(ctx context.Context, req *types.QueryGetDisplayNameAppealStakeRequest) (*types.QueryGetDisplayNameAppealStakeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.DisplayNameAppealStake.Get(ctx, req.ChallengeId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetDisplayNameAppealStakeResponse{DisplayNameAppealStake: val}, nil
}
