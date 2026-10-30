package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetProposalVoterTree returns a secret-ballot proposal's voter snapshot
// while voting is open. Fetch it whole and build the Merkle path locally:
// asking a node for one member's path would tell it who is about to vote.
func (q queryServer) GetProposalVoterTree(ctx context.Context, req *types.QueryGetProposalVoterTreeRequest) (*types.QueryGetProposalVoterTreeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	tree, err := q.k.ProposalVoterTrees.Get(ctx, req.ProposalId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "proposal %d has no open secret ballot", req.ProposalId)
	}
	return &types.QueryGetProposalVoterTreeResponse{Tree: tree}, nil
}
