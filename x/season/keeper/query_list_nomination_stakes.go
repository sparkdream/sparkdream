package keeper

import (
	"context"
	"fmt"
	"strings"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListNominationStakes returns all stakes for a given nomination.
func (q queryServer) ListNominationStakes(ctx context.Context, req *types.QueryListNominationStakesRequest) (*types.QueryListNominationStakesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// NominationStake keys are formatted as "nominationId/staker"
	prefix := fmt.Sprintf("%d/", req.NominationId)

	iter, err := q.k.NominationStake.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	var stakes []types.NominationStake
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			continue
		}
		if strings.HasPrefix(key, prefix) {
			stake, err := iter.Value()
			if err != nil {
				continue
			}
			stakes = append(stakes, stake)
		}
	}

	return &types.QueryListNominationStakesResponse{
		Stakes: stakes,
	}, nil
}
