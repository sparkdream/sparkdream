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

func (q queryServer) ListThreadMetadata(ctx context.Context, req *types.QueryAllThreadMetadataRequest) (*types.QueryAllThreadMetadataResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	threadMetadatas, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.ThreadMetadata,
		req.Pagination,
		func(_ uint64, value types.ThreadMetadata) (types.ThreadMetadata, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllThreadMetadataResponse{ThreadMetadata: threadMetadatas, Pagination: pageRes}, nil
}

func (q queryServer) GetThreadMetadata(ctx context.Context, req *types.QueryGetThreadMetadataRequest) (*types.QueryGetThreadMetadataResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.ThreadMetadata.Get(ctx, req.ThreadId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetThreadMetadataResponse{ThreadMetadata: val}, nil
}
