package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) FlaggedContent(ctx context.Context, req *types.QueryFlaggedContentRequest) (*types.QueryFlaggedContentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var results []types.CollectionFlag
	pageReq := req.Pagination
	if pageReq == nil {
		pageReq = &query.PageRequest{Limit: 100}
	}
	limit := pageReq.Limit
	if limit == 0 {
		limit = 100
	}
	offset := pageReq.Offset
	var count uint64

	// Walk FlagReviewQueue index, get each CollectionFlag
	err := q.k.FlagReviewQueue.Walk(ctx, nil,
		func(key collections.Pair[int32, uint64]) (bool, error) {
			if count < offset {
				count++
				return false, nil
			}
			if uint64(len(results)) >= limit {
				return true, nil
			}
			// Build the flag key from the review queue entry
			targetType := types.FlagTargetType(key.K1())
			targetID := key.K2()
			flagKey := FlagCompositeKey(targetType, targetID)
			flag, err := q.k.Flag.Get(ctx, flagKey)
			if err != nil {
				count++
				return false, nil
			}
			results = append(results, flag)
			count++
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryFlaggedContentResponse{
		CollectionFlags: results,
		Pagination:      &query.PageResponse{Total: count},
	}, nil
}
