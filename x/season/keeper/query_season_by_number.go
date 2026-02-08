package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) SeasonByNumber(ctx context.Context, req *types.QuerySeasonByNumberRequest) (*types.QuerySeasonByNumberResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Check if this is the current season
	currentSeason, err := q.k.Season.Get(ctx)
	if err == nil && currentSeason.Number == req.Number {
		return &types.QuerySeasonByNumberResponse{
			Name:       currentSeason.Name,
			Theme:      currentSeason.Theme,
			StartBlock: currentSeason.StartBlock,
			EndBlock:   currentSeason.EndBlock,
			Status:     uint64(currentSeason.Status),
		}, nil
	}

	// Look up in season snapshots for historical seasons
	snapshot, err := q.k.SeasonSnapshot.Get(ctx, req.Number)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "season %d not found", req.Number)
	}

	// SeasonSnapshot only contains Season and SnapshotBlock
	// For historical seasons, we return limited information
	return &types.QuerySeasonByNumberResponse{
		Name:       "",                                                 // Not stored in snapshot
		Theme:      "",                                                 // Not stored in snapshot
		StartBlock: 0,                                                  // Not stored in snapshot
		EndBlock:   snapshot.SnapshotBlock,                             // Use snapshot block as end
		Status:     uint64(types.SeasonStatus_SEASON_STATUS_COMPLETED), // Historical = completed
	}, nil
}
