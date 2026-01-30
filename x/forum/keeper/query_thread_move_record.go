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

func (q queryServer) ListThreadMoveRecord(ctx context.Context, req *types.QueryAllThreadMoveRecordRequest) (*types.QueryAllThreadMoveRecordResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	threadMoveRecords, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.ThreadMoveRecord,
		req.Pagination,
		func(_ uint64, value types.ThreadMoveRecord) (types.ThreadMoveRecord, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllThreadMoveRecordResponse{ThreadMoveRecord: threadMoveRecords, Pagination: pageRes}, nil
}

func (q queryServer) GetThreadMoveRecord(ctx context.Context, req *types.QueryGetThreadMoveRecordRequest) (*types.QueryGetThreadMoveRecordResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.ThreadMoveRecord.Get(ctx, req.RootId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetThreadMoveRecordResponse{ThreadMoveRecord: val}, nil
}
