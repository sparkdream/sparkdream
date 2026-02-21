package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ProposalVotes(ctx context.Context, req *types.QueryProposalVotesRequest) (*types.QueryProposalVotesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	prefix := fmt.Sprintf("%d/", req.ProposalId)

	var votes []types.AnonymousVote
	var sealedVotes []types.SealedVote

	// Collect anonymous votes for this proposal.
	err := q.k.AnonymousVote.Walk(ctx, nil, func(key string, v types.AnonymousVote) (bool, error) {
		if v.ProposalId == req.ProposalId || (len(key) >= len(prefix) && key[:len(prefix)] == prefix) {
			votes = append(votes, v)
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Collect sealed votes for this proposal.
	err = q.k.SealedVote.Walk(ctx, nil, func(key string, sv types.SealedVote) (bool, error) {
		if sv.ProposalId == req.ProposalId || (len(key) >= len(prefix) && key[:len(prefix)] == prefix) {
			sealedVotes = append(sealedVotes, sv)
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryProposalVotesResponse{
		Votes:       votes,
		SealedVotes: sealedVotes,
	}, nil
}
