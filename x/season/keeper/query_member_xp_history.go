package keeper

import (
	"context"
	"strconv"
	"strings"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MemberXpHistory(ctx context.Context, req *types.QueryMemberXpHistoryRequest) (*types.QueryMemberXpHistoryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "member address required")
	}

	// Get XP tracking entries for this member from EpochXpTracker
	// Key format is "member:epoch"
	iter, err := q.k.EpochXpTracker.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	var latestEpoch int64
	var latestXpEarned uint64
	var cumulativeXp uint64

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			continue
		}
		// Check if this entry belongs to the requested member
		// Key format is "member:epoch"
		if strings.HasPrefix(key, req.Address+":") {
			tracker, err := iter.Value()
			if err != nil {
				continue
			}
			// Sum all XP types for this epoch
			epochXp := tracker.VoteXpEarned + tracker.ForumXpEarned + tracker.QuestXpEarned + tracker.OtherXpEarned
			cumulativeXp += epochXp

			// Extract epoch from key and track latest
			parts := strings.SplitN(key, ":", 2)
			if len(parts) == 2 {
				if epoch, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					if epoch > latestEpoch {
						latestEpoch = epoch
						latestXpEarned = epochXp
					}
				}
			}
		}
	}

	// Get cumulative XP from profile if available
	profile, err := q.k.MemberProfile.Get(ctx, req.Address)
	if err == nil {
		cumulativeXp = profile.SeasonXp // Use actual season XP
	}

	return &types.QueryMemberXpHistoryResponse{
		Epoch:        latestEpoch,
		XpEarned:     latestXpEarned,
		CumulativeXp: cumulativeXp,
	}, nil
}
