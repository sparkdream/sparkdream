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

func (q queryServer) ListTitle(ctx context.Context, req *types.QueryAllTitleRequest) (*types.QueryAllTitleResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	titles, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Title,
		req.Pagination,
		func(_ string, value types.Title) (types.Title, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllTitleResponse{Title: titles, Pagination: pageRes}, nil
}

func (q queryServer) GetTitle(ctx context.Context, req *types.QueryGetTitleRequest) (*types.QueryGetTitleResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.Title.Get(ctx, req.TitleId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetTitleResponse{Title: val}, nil
}
