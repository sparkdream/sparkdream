package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListRetroRewardHistory returns retroactive reward records for a given season.
func (q queryServer) ListRetroRewardHistory(ctx context.Context, req *types.QueryListRetroRewardHistoryRequest) (*types.QueryListRetroRewardHistoryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// RetroRewardRecord keys are "season/nominationId". Walk only this
	// season's key range ('0' is the byte after '/') with key-based
	// pagination (SEA-35), never the whole history.
	prefix := fmt.Sprintf("%d/", req.Season)
	rng := new(collections.Range[string]).StartInclusive(prefix).EndExclusive(fmt.Sprintf("%d0", req.Season))
	limit := uint64(defaultRetroHistoryLimit)
	if req.Pagination != nil {
		if len(req.Pagination.Key) > 0 {
			rng = new(collections.Range[string]).StartExclusive(string(req.Pagination.Key)).EndExclusive(fmt.Sprintf("%d0", req.Season))
		}
		if req.Pagination.Limit > 0 {
			limit = req.Pagination.Limit
		}
	}
	if limit > maxRetroHistoryLimit {
		limit = maxRetroHistoryLimit
	}

	var records []types.RetroRewardRecord
	var nextKey []byte
	var lastKey string
	if err := q.k.RetroRewardRecord.Walk(ctx, rng, func(key string, record types.RetroRewardRecord) (bool, error) {
		if uint64(len(records)) >= limit {
			nextKey = []byte(lastKey)
			return true, nil
		}
		records = append(records, record)
		lastKey = key
		return false, nil
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListRetroRewardHistoryResponse{
		Records:    records,
		Pagination: &query.PageResponse{NextKey: nextKey},
	}, nil
}

const (
	defaultRetroHistoryLimit = 100
	maxRetroHistoryLimit     = 1000
)
