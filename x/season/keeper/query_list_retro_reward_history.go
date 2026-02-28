package keeper

import (
	"context"
	"fmt"
	"strings"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListRetroRewardHistory returns retroactive reward records for a given season.
func (q queryServer) ListRetroRewardHistory(ctx context.Context, req *types.QueryListRetroRewardHistoryRequest) (*types.QueryListRetroRewardHistoryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// RetroRewardRecord keys are formatted as "season/nominationId"
	prefix := fmt.Sprintf("%d/", req.Season)

	iter, err := q.k.RetroRewardRecord.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	var records []types.RetroRewardRecord
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			continue
		}
		if strings.HasPrefix(key, prefix) {
			record, err := iter.Value()
			if err != nil {
				continue
			}
			records = append(records, record)
		}
	}

	return &types.QueryListRetroRewardHistoryResponse{
		Records: records,
	}, nil
}
