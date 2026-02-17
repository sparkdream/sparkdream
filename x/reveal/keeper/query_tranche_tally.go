package keeper

import (
	"context"

	"sparkdream/x/reveal/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) TrancheTally(ctx context.Context, req *types.QueryTrancheTallyRequest) (*types.QueryTrancheTallyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Verify contribution exists
	_, err := q.k.Contribution.Get(ctx, req.ContributionId)
	if err != nil {
		return nil, status.Error(codes.NotFound, types.ErrContributionNotFound.Error())
	}

	yesWeight, noWeight, voteCount, err := q.k.tallyVotes(ctx, req.ContributionId, req.TrancheId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryTrancheTallyResponse{
		YesWeight: yesWeight,
		NoWeight:  noWeight,
		VoteCount: voteCount,
	}, nil
}
