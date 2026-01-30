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

func (q queryServer) ListHideRecord(ctx context.Context, req *types.QueryAllHideRecordRequest) (*types.QueryAllHideRecordResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	hideRecords, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.HideRecord,
		req.Pagination,
		func(_ uint64, value types.HideRecord) (types.HideRecord, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllHideRecordResponse{HideRecord: hideRecords, Pagination: pageRes}, nil
}

func (q queryServer) GetHideRecord(ctx context.Context, req *types.QueryGetHideRecordRequest) (*types.QueryGetHideRecordResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.HideRecord.Get(ctx, req.PostId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetHideRecordResponse{HideRecord: val}, nil
}
