package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) SeasonStats(ctx context.Context, req *types.QuerySeasonStatsRequest) (*types.QuerySeasonStatsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentSeason, err := q.k.Season.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.NotFound, "season not found")
	}

	// If requesting a specific season and it's not the current one, return stored stats
	if req.Season != 0 && req.Season != currentSeason.Number {
		// Historical season stats would come from snapshot
		snapshot, err := q.k.SeasonSnapshot.Get(ctx, req.Season)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "season %d not found", req.Season)
		}
		// Historical snapshots only have basic data; return with zeros for detailed stats
		return &types.QuerySeasonStatsResponse{
			TotalXpEarned:        0, // Would need to store in snapshot
			ActiveMembers:        0,
			InitiativesCompleted: 0,
			GuildsActive:         0,
			QuestsCompleted:      0,
			BlocksRemaining:      snapshot.SnapshotBlock - currentSeason.StartBlock,
		}, nil
	}

	// Calculate current season stats
	var totalXpEarned uint64
	var activeMembers uint64
	var questsCompleted uint64

	// Count active members and total XP from member profiles
	profileIter, err := q.k.MemberProfile.Iterate(ctx, nil)
	if err == nil {
		defer profileIter.Close()
		for ; profileIter.Valid(); profileIter.Next() {
			profile, err := profileIter.Value()
			if err != nil {
				continue
			}
			activeMembers++
			totalXpEarned += profile.SeasonXp
		}
	}

	// Count completed quests
	questProgressIter, err := q.k.MemberQuestProgress.Iterate(ctx, nil)
	if err == nil {
		defer questProgressIter.Close()
		for ; questProgressIter.Valid(); questProgressIter.Next() {
			progress, err := questProgressIter.Value()
			if err != nil {
				continue
			}
			if progress.Completed {
				questsCompleted++
			}
		}
	}

	// Count active guilds
	var guildsActive uint64
	guildIter, err := q.k.Guild.Iterate(ctx, nil)
	if err == nil {
		defer guildIter.Close()
		for ; guildIter.Valid(); guildIter.Next() {
			guild, err := guildIter.Value()
			if err != nil {
				continue
			}
			if guild.Status == types.GuildStatus_GUILD_STATUS_ACTIVE {
				guildsActive++
			}
		}
	}

	// Calculate blocks remaining
	blocksRemaining := currentSeason.EndBlock - sdkCtx.BlockHeight()
	if blocksRemaining < 0 {
		blocksRemaining = 0
	}

	return &types.QuerySeasonStatsResponse{
		TotalXpEarned:        totalXpEarned,
		ActiveMembers:        activeMembers,
		InitiativesCompleted: 0, // Would need x/rep integration
		GuildsActive:         guildsActive,
		QuestsCompleted:      questsCompleted,
		BlocksRemaining:      blocksRemaining,
	}, nil
}
