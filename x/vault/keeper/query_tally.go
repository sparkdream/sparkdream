package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vault/types"
)

func (q queryServer) Tally(ctx context.Context, req *types.QueryTallyRequest) (*types.QueryTallyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	p, err := q.k.VaultProposal.Get(ctx, req.ProposalId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "proposal not found")
	}
	v, err := q.k.Vault.Get(ctx, p.VaultId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "vault not found")
	}
	approved := q.k.Tally(ctx, v, p)
	met := p.Status == types.ProposalStatus_PROPOSAL_STATUS_PENDING && p.ConfigVersion == v.ConfigVersion && approved >= v.Threshold
	return &types.QueryTallyResponse{ApprovedWeight: approved, Threshold: v.Threshold, Met: met}, nil
}
