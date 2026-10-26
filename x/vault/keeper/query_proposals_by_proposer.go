package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vault/types"
)

// ProposalsByProposer pages over every stored proposal opened by the address
// or any x/rep alias of it, across vaults, optionally filtered by status
// (UNSPECIFIED = all). Resolved proposals leave the store after
// ProposalRetention.
func (q queryServer) ProposalsByProposer(ctx context.Context, req *types.QueryProposalsByProposerRequest) (*types.QueryProposalsByProposerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if _, err := q.k.addressCodec.StringToBytes(req.Proposer); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid address")
	}
	proposers := map[string]bool{}
	for _, addr := range q.k.allAddressesOf(ctx, req.Proposer) {
		proposers[addr] = true
	}
	proposals, pageRes, err := query.CollectionFilteredPaginate(
		ctx, q.k.VaultProposal, req.Pagination,
		func(_ uint64, p types.VaultProposal) (bool, error) {
			if !proposers[p.Proposer] {
				return false, nil
			}
			return req.Status == types.ProposalStatus_PROPOSAL_STATUS_UNSPECIFIED || p.Status == req.Status, nil
		},
		func(_ uint64, p types.VaultProposal) (types.VaultProposal, error) { return p, nil },
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryProposalsByProposerResponse{Proposals: proposals, Pagination: pageRes}, nil
}
