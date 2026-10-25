package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/reveal/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) StakesByStaker(ctx context.Context, req *types.QueryStakesByStakerRequest) (*types.QueryStakesByStakerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var stakes []types.RevealStake
	// Records stay indexed under the address that wrote them; a migrated
	// member's are gathered across every alias (unpaginated, as before).
	for _, staker := range q.k.allAddressesOf(ctx, req.Staker) {
		err := q.k.StakesByStaker.Walk(ctx,
			collections.NewPrefixedPairRange[string, uint64](staker),
			func(key collections.Pair[string, uint64]) (bool, error) {
				stake, err := q.k.RevealStake.Get(ctx, key.K2())
				if err != nil {
					return false, nil // skip missing
				}
				stakes = append(stakes, stake)
				return false, nil
			},
		)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &types.QueryStakesByStakerResponse{
		Stakes: stakes,
	}, nil
}
