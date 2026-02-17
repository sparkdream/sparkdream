package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/reveal/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) VotesByVoter(ctx context.Context, req *types.QueryVotesByVoterRequest) (*types.QueryVotesByVoterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var votes []types.VerificationVote
	err := q.k.VotesByVoter.Walk(ctx,
		collections.NewPrefixedPairRange[string, string](req.Voter),
		func(key collections.Pair[string, string]) (bool, error) {
			vote, err := q.k.Vote.Get(ctx, key.K2())
			if err != nil {
				return false, nil // skip missing
			}
			votes = append(votes, vote)
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryVotesByVoterResponse{
		Votes: votes,
	}, nil
}
