package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SeasonStats reads the running per-season counters and the active-guild
// counter (SEA-35): no store scans. A past season returns its archived
// counters with guilds_active and blocks_remaining 0.
func (q queryServer) SeasonStats(ctx context.Context, req *types.QuerySeasonStatsRequest) (*types.QuerySeasonStatsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentSeason, err := q.k.Season.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.NotFound, "season not found")
	}

	if req.Season != 0 && req.Season != currentSeason.Number {
		if _, err := q.k.SeasonSnapshot.Get(ctx, req.Season); err != nil {
			return nil, status.Errorf(codes.NotFound, "season %d not found", req.Season)
		}
		c, err := q.k.GetSeasonStatsCounters(ctx, req.Season)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &types.QuerySeasonStatsResponse{
			TotalXpEarned:        c.TotalXpEarned,
			ActiveMembers:        c.ActiveMembers,
			InitiativesCompleted: c.InitiativesCompleted,
			QuestsCompleted:      c.QuestsCompleted,
			BlocksRemaining:      0,
		}, nil
	}

	c, err := q.k.getCurrentSeasonStats(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	guildsActive, err := q.k.Guild.Indexes.ActiveCount.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	blocksRemaining := currentSeason.EndBlock - sdkCtx.BlockHeight()
	if blocksRemaining < 0 {
		blocksRemaining = 0
	}

	return &types.QuerySeasonStatsResponse{
		TotalXpEarned:        c.TotalXpEarned,
		ActiveMembers:        c.ActiveMembers,
		InitiativesCompleted: c.InitiativesCompleted,
		GuildsActive:         guildsActive,
		QuestsCompleted:      c.QuestsCompleted,
		BlocksRemaining:      blocksRemaining,
	}, nil
}
