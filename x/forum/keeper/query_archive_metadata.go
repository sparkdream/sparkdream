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

func (q queryServer) ListArchiveMetadata(ctx context.Context, req *types.QueryAllArchiveMetadataRequest) (*types.QueryAllArchiveMetadataResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	archiveMetadatas, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.ArchiveMetadata,
		req.Pagination,
		func(_ uint64, value types.ArchiveMetadata) (types.ArchiveMetadata, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllArchiveMetadataResponse{ArchiveMetadata: archiveMetadatas, Pagination: pageRes}, nil
}

func (q queryServer) GetArchiveMetadata(ctx context.Context, req *types.QueryGetArchiveMetadataRequest) (*types.QueryGetArchiveMetadataResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.ArchiveMetadata.Get(ctx, req.RootId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetArchiveMetadataResponse{ArchiveMetadata: val}, nil
}
