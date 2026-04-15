package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GetVerificationRecord(ctx context.Context, req *types.QueryGetVerificationRecordRequest) (*types.QueryGetVerificationRecordResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	record, err := q.k.VerificationRecords.Get(ctx, req.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "no verification record for content ID %d", req.ContentId)
	}

	return &types.QueryGetVerificationRecordResponse{Record: record}, nil
}
