package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
	// met mirrors pendingProposal + Execute: a proposal past expires_at is
	// dead even before the EndBlocker's capped pass marks it EXPIRED.
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	met := p.Status == types.ProposalStatus_PROPOSAL_STATUS_PENDING && p.ConfigVersion == v.ConfigVersion &&
		now < p.ExpiresAt && approved >= v.Threshold
	return &types.QueryTallyResponse{ApprovedWeight: approved, Threshold: v.Threshold, Met: met}, nil
}
