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

func (q queryServer) ListTleDecryptionShare(ctx context.Context, req *types.QueryAllTleDecryptionShareRequest) (*types.QueryAllTleDecryptionShareResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	tleDecryptionShares, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.TleDecryptionShare,
		req.Pagination,
		func(_ string, value types.TleDecryptionShare) (types.TleDecryptionShare, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllTleDecryptionShareResponse{TleDecryptionShare: tleDecryptionShares, Pagination: pageRes}, nil
}

func (q queryServer) GetTleDecryptionShare(ctx context.Context, req *types.QueryGetTleDecryptionShareRequest) (*types.QueryGetTleDecryptionShareResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.TleDecryptionShare.Get(ctx, req.Index)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetTleDecryptionShareResponse{TleDecryptionShare: val}, nil
}
