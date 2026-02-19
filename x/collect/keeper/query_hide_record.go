package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) HideRecord(ctx context.Context, req *types.QueryHideRecordRequest) (*types.QueryHideRecordResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	hr, err := q.k.HideRecord.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, types.ErrHideRecordNotFound.Error())
	}

	return &types.QueryHideRecordResponse{HideRecord: hr}, nil
}
