package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) HideRecordsByTarget(ctx context.Context, req *types.QueryHideRecordsByTargetRequest) (*types.QueryHideRecordsByTargetResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	var records []types.HideRecord
	err := q.k.HidesByTargetAll.Walk(ctx,
		collections.NewSuperPrefixedTripleRange[uint64, uint64, uint64](req.ClassId, req.TokenId),
		func(key collections.Triple[uint64, uint64, uint64]) (bool, error) {
			hr, err := q.k.HideRecords.Get(ctx, key.K3())
			if err != nil {
				return true, err
			}
			records = append(records, hr)
			return len(records) >= maxPageLimit, nil
		})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryHideRecordsByTargetResponse{Records: records}, nil
}
