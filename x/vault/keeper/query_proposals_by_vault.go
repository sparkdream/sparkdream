package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vault/types"
)

// ProposalsByVault lists the proposals of a vault, optionally filtered by
// status (UNSPECIFIED = all), through the ProposalsByVault index (a prefix
// walk over the vault's own proposals, never the whole store). Resolved
// proposals leave the store after ProposalRetention.
func (q queryServer) ProposalsByVault(ctx context.Context, req *types.QueryProposalsByVaultRequest) (*types.QueryProposalsByVaultResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if _, err := q.k.Vault.Get(ctx, req.VaultId); err != nil {
		return nil, status.Error(codes.NotFound, "vault not found")
	}
	proposals, pageRes, err := query.CollectionFilteredPaginate(
		ctx, q.k.ProposalsByVault, req.Pagination,
		func(key collections.Pair[uint64, uint64], _ collections.NoValue) (bool, error) {
			if req.Status == types.ProposalStatus_PROPOSAL_STATUS_UNSPECIFIED {
				return true, nil
			}
			p, err := q.k.VaultProposal.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			return p.Status == req.Status, nil
		},
		func(key collections.Pair[uint64, uint64], _ collections.NoValue) (types.VaultProposal, error) {
			return q.k.VaultProposal.Get(ctx, key.K2())
		},
		query.WithCollectionPaginationPairPrefix[uint64, uint64](req.VaultId),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryProposalsByVaultResponse{Proposals: proposals, Pagination: pageRes}, nil
}
