package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) CurrentSeason(ctx context.Context, req *types.QueryCurrentSeasonRequest) (*types.QueryCurrentSeasonResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	season, err := q.k.Season.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.NotFound, "season not found")
	}

	return &types.QueryCurrentSeasonResponse{
		Number:     season.Number,
		Name:       season.Name,
		Theme:      season.Theme,
		StartBlock: season.StartBlock,
		EndBlock:   season.EndBlock,
		Status:     uint64(season.Status),
	}, nil
}
