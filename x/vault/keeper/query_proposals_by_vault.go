package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vault/types"
)

// ProposalsByVault pages over every stored proposal of the vault, optionally
// filtered by status (UNSPECIFIED = all). Resolved proposals leave the store
// after ProposalRetention.
func (q queryServer) ProposalsByVault(ctx context.Context, req *types.QueryProposalsByVaultRequest) (*types.QueryProposalsByVaultResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if _, err := q.k.Vault.Get(ctx, req.VaultId); err != nil {
		return nil, status.Error(codes.NotFound, "vault not found")
	}
	proposals, pageRes, err := query.CollectionFilteredPaginate(
		ctx, q.k.VaultProposal, req.Pagination,
		func(_ uint64, p types.VaultProposal) (bool, error) {
			if p.VaultId != req.VaultId {
				return false, nil
			}
			return req.Status == types.ProposalStatus_PROPOSAL_STATUS_UNSPECIFIED || p.Status == req.Status, nil
		},
		func(_ uint64, p types.VaultProposal) (types.VaultProposal, error) { return p, nil },
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryProposalsByVaultResponse{Proposals: proposals, Pagination: pageRes}, nil
}
