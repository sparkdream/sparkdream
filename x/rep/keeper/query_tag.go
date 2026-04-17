package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/types"
)

func (q queryServer) ListTag(ctx context.Context, req *types.QueryAllTagRequest) (*types.QueryAllTagResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	tags, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Tag,
		req.Pagination,
		func(_ string, value types.Tag) (types.Tag, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllTagResponse{Tag: tags, Pagination: pageRes}, nil
}

func (q queryServer) GetTag(ctx context.Context, req *types.QueryGetTagRequest) (*types.QueryGetTagResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.Tag.Get(ctx, req.Name)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetTagResponse{Tag: val}, nil
}
