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

func (q queryServer) ListEpochDecryptionKey(ctx context.Context, req *types.QueryAllEpochDecryptionKeyRequest) (*types.QueryAllEpochDecryptionKeyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	epochDecryptionKeys, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.EpochDecryptionKey,
		req.Pagination,
		func(_ uint64, value types.EpochDecryptionKey) (types.EpochDecryptionKey, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllEpochDecryptionKeyResponse{EpochDecryptionKey: epochDecryptionKeys, Pagination: pageRes}, nil
}

func (q queryServer) GetEpochDecryptionKey(ctx context.Context, req *types.QueryGetEpochDecryptionKeyRequest) (*types.QueryGetEpochDecryptionKeyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.EpochDecryptionKey.Get(ctx, req.Epoch)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetEpochDecryptionKeyResponse{EpochDecryptionKey: val}, nil
}
