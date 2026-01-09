package keeper

import (
	"context"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListInterim(ctx context.Context, req *types.QueryAllInterimRequest) (*types.QueryAllInterimResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	interims, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Interim,
		req.Pagination,
		func(_ uint64, value types.Interim) (types.Interim, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllInterimResponse{Interim: interims, Pagination: pageRes}, nil
}

func (q queryServer) GetInterim(ctx context.Context, req *types.QueryGetInterimRequest) (*types.QueryGetInterimResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	interim, err := q.k.Interim.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetInterimResponse{Interim: interim}, nil
}
