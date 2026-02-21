package keeper

import (
	"context"
	"errors"

	"sparkdream/x/vote/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListTleValidatorShare(ctx context.Context, req *types.QueryAllTleValidatorShareRequest) (*types.QueryAllTleValidatorShareResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	tleValidatorShares, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.TleValidatorShare,
		req.Pagination,
		func(_ string, value types.TleValidatorShare) (types.TleValidatorShare, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllTleValidatorShareResponse{TleValidatorShare: tleValidatorShares, Pagination: pageRes}, nil
}

func (q queryServer) GetTleValidatorShare(ctx context.Context, req *types.QueryGetTleValidatorShareRequest) (*types.QueryGetTleValidatorShareResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.TleValidatorShare.Get(ctx, req.Validator)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetTleValidatorShareResponse{TleValidatorShare: val}, nil
}
