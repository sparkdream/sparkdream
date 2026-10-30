package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetPollVotes lists the open votes on a poll (a secret-ballot poll has
// none: only its tallies).
func (q queryServer) GetPollVotes(ctx context.Context, req *types.QueryGetPollVotesRequest) (*types.QueryGetPollVotesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	votes, pageRes, err := query.CollectionPaginate(ctx, q.k.PollVotes, req.Pagination,
		func(_ collections.Pair[uint64, string], v types.PollVote) (types.PollVote, error) { return v, nil },
		query.WithCollectionPaginationPairPrefix[uint64, string](req.PollId),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryGetPollVotesResponse{Votes: votes, Pagination: pageRes}, nil
}
