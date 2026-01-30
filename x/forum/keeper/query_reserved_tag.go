package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListReservedTag(ctx context.Context, req *types.QueryAllReservedTagRequest) (*types.QueryAllReservedTagResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	reservedTags, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.ReservedTag,
		req.Pagination,
		func(_ string, value types.ReservedTag) (types.ReservedTag, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllReservedTagResponse{ReservedTag: reservedTags, Pagination: pageRes}, nil
}

func (q queryServer) GetReservedTag(ctx context.Context, req *types.QueryGetReservedTagRequest) (*types.QueryGetReservedTagResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.ReservedTag.Get(ctx, req.Name)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetReservedTagResponse{ReservedTag: val}, nil
}
