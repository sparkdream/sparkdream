package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetPollVoterTree returns a secret-ballot poll's voter snapshot while the
// poll is open (see GetProposalVoterTree).
func (q queryServer) GetPollVoterTree(ctx context.Context, req *types.QueryGetPollVoterTreeRequest) (*types.QueryGetPollVoterTreeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	tree, err := q.k.PollVoterTrees.Get(ctx, req.PollId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "poll %d has no open secret ballot", req.PollId)
	}
	return &types.QueryGetPollVoterTreeResponse{Tree: tree}, nil
}
