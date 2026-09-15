package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// defaultFlaggedContentLimit is the page size used when the caller sends no
// pagination block (or an explicit limit of 0).
const defaultFlaggedContentLimit = 100

// FlaggedContent returns the CollectionFlags currently sitting in the
// sentinel review queue.
//
// Paging is offset-based: the walk always runs to completion so Total is the
// real size of the queue, which is what lets a client page past the first
// page. Total is deliberately not short-circuited at offset+limit — a
// truncated Total would tell the caller there is nothing more to fetch.
// NextKey is left unset because this endpoint does not accept
// PageRequest.Key; advance with Offset.
func (q queryServer) FlaggedContent(ctx context.Context, req *types.QueryFlaggedContentRequest) (*types.QueryFlaggedContentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var results []types.CollectionFlag
	pageReq := req.Pagination
	if pageReq == nil {
		pageReq = &query.PageRequest{}
	}
	limit := pageReq.Limit
	if limit == 0 {
		limit = defaultFlaggedContentLimit
	}
	offset := pageReq.Offset

	// total counts the queue entries that resolve to a live CollectionFlag,
	// i.e. the rows a caller can actually page through.
	var total uint64

	// Walk FlagReviewQueue index, get each CollectionFlag
	err := q.k.FlagReviewQueue.Walk(ctx, nil,
		func(key collections.Pair[int32, uint64]) (bool, error) {
			// Build the flag key from the review queue entry
			targetType := types.FlagTargetType(key.K1())
			targetID := key.K2()
			flagKey := FlagCompositeKey(targetType, targetID)
			flag, err := q.k.Flag.Get(ctx, flagKey)
			if err != nil {
				// Orphaned queue row (flag cleared without its index entry).
				// It is not part of the result set, so it must not advance
				// the offset or inflate the total.
				return false, nil
			}
			if total >= offset && uint64(len(results)) < limit {
				results = append(results, flag)
			}
			total++
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryFlaggedContentResponse{
		CollectionFlags: results,
		Pagination:      &query.PageResponse{Total: total},
	}, nil
}
