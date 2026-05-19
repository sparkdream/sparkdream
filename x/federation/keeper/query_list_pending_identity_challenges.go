package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListPendingIdentityChallenges(ctx context.Context, req *types.QueryListPendingIdentityChallengesRequest) (*types.QueryListPendingIdentityChallengesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	challenges, pageRes, err := query.CollectionPaginate(
		ctx, q.k.PendingIdChallenges, req.Pagination,
		func(_ collections.Pair[string, string], value types.PendingIdentityChallenge) (types.PendingIdentityChallenge, error) {
			return value, nil
		},
		query.WithCollectionPaginationPairPrefix[string, string](req.ClaimedAddress),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListPendingIdentityChallengesResponse{
		Challenges: challenges,
		Pagination: pageRes,
	}, nil
}
