package keeper

import (
	"context"
	"errors"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SeasonByNumber returns the live season, a completed season's stored
// record (SeasonHistory, kept for snapshot_retention_seasons seasons) or,
// for a season older than the history, the little its SeasonSnapshot still
// tells: no name or theme, the snapshot block as end block, status COMPLETED.
func (q queryServer) SeasonByNumber(ctx context.Context, req *types.QuerySeasonByNumberRequest) (*types.QuerySeasonByNumberResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Check if this is the current season
	currentSeason, err := q.k.Season.Get(ctx)
	if err == nil && currentSeason.Number == req.Number {
		return seasonByNumberResponse(currentSeason), nil
	}

	past, err := q.k.SeasonHistory.Get(ctx, req.Number)
	if err == nil {
		return seasonByNumberResponse(past), nil
	}
	if !errors.Is(err, collections.ErrNotFound) {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Older than the history: fall back to the season snapshot.
	snapshot, err := q.k.SeasonSnapshot.Get(ctx, req.Number)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "season %d not found", req.Number)
	}

	// SeasonSnapshot only contains Season and SnapshotBlock
	return &types.QuerySeasonByNumberResponse{
		Name:       "",                                                 // Not stored in snapshot
		Theme:      "",                                                 // Not stored in snapshot
		StartBlock: 0,                                                  // Not stored in snapshot
		EndBlock:   snapshot.SnapshotBlock,                             // Use snapshot block as end
		Status:     uint64(types.SeasonStatus_SEASON_STATUS_COMPLETED), // Historical = completed
	}, nil
}

func seasonByNumberResponse(season types.Season) *types.QuerySeasonByNumberResponse {
	return &types.QuerySeasonByNumberResponse{
		Name:       season.Name,
		Theme:      season.Theme,
		StartBlock: season.StartBlock,
		EndBlock:   season.EndBlock,
		Status:     commontypes.Saturate[uint64](season.Status),
	}
}
