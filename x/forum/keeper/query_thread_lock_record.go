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

func (q queryServer) ListThreadLockRecord(ctx context.Context, req *types.QueryAllThreadLockRecordRequest) (*types.QueryAllThreadLockRecordResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	threadLockRecords, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.ThreadLockRecord,
		req.Pagination,
		func(_ uint64, value types.ThreadLockRecord) (types.ThreadLockRecord, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllThreadLockRecordResponse{ThreadLockRecord: threadLockRecords, Pagination: pageRes}, nil
}

func (q queryServer) GetThreadLockRecord(ctx context.Context, req *types.QueryGetThreadLockRecordRequest) (*types.QueryGetThreadLockRecordResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.ThreadLockRecord.Get(ctx, req.RootId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetThreadLockRecordResponse{ThreadLockRecord: val}, nil
}
