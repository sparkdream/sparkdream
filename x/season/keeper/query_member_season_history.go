package keeper

import (
	"context"
	"strings"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MemberSeasonHistory(ctx context.Context, req *types.QueryMemberSeasonHistoryRequest) (*types.QueryMemberSeasonHistoryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "member address required")
	}

	// Check if we have any season snapshots for this member
	// Keys are formatted as "seasonNumber/address"
	iter, err := q.k.MemberSeasonSnapshot.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	// Find the most recent snapshot for this member
	var latestSeason uint64
	var latestXpEarned uint64
	var latestLevel uint64

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			continue
		}
		// Check if this snapshot belongs to the requested member
		// Key format is "seasonNumber/address" - check suffix for address
		if strings.HasSuffix(key, "/"+req.Address) {
			snapshot, err := iter.Value()
			if err != nil {
				continue
			}
			// Track latest snapshot
			latestXpEarned = snapshot.XpEarned
			latestLevel = snapshot.SeasonLevel
			// Extract season from key (before the /)
			parts := strings.SplitN(key, "/", 2)
			if len(parts) >= 1 {
				// Parse season number from key
				var seasonNum uint64
				if _, err := parseSeasonFromKey(parts[0], &seasonNum); err == nil && seasonNum > latestSeason {
					latestSeason = seasonNum
				}
			}
		}
	}

	// If no snapshot found, check current profile
	if latestSeason == 0 {
		profile, err := q.k.MemberProfile.Get(ctx, req.Address)
		if err != nil {
			return &types.QueryMemberSeasonHistoryResponse{}, nil
		}
		// Get current season
		season, _ := q.k.Season.Get(ctx)
		return &types.QueryMemberSeasonHistoryResponse{
			Season:   season.Number,
			XpEarned: profile.SeasonXp,
			Level:    q.k.CalculateLevel(ctx, profile.SeasonXp),
		}, nil
	}

	return &types.QueryMemberSeasonHistoryResponse{
		Season:   latestSeason,
		XpEarned: latestXpEarned,
		Level:    latestLevel,
	}, nil
}

// Helper to parse season number from key part
func parseSeasonFromKey(s string, seasonNum *uint64) (bool, error) {
	var num uint64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			num = num*10 + uint64(c-'0')
		} else {
			return false, nil
		}
	}
	*seasonNum = num
	return true, nil
}
