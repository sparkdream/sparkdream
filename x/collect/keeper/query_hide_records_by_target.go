package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) HideRecordsByTarget(ctx context.Context, req *types.QueryHideRecordsByTargetRequest) (*types.QueryHideRecordsByTargetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var results []types.HideRecord
	targetKey := HideRecordTargetCompositeKey(req.TargetType, req.TargetId)

	// Walk HideRecordByTarget index with prefix for the target
	err := q.k.HideRecordByTarget.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](targetKey),
		func(key collections.Pair[string, uint64]) (bool, error) {
			hr, err := q.k.HideRecord.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			results = append(results, hr)
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryHideRecordsByTargetResponse{HideRecords: results}, nil
}
