package keeper

import (
	"context"
	"errors"

	"sparkdream/x/vault/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListVaultProposal(ctx context.Context, req *types.QueryAllVaultProposalRequest) (*types.QueryAllVaultProposalResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	vaultProposals, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.VaultProposal,
		req.Pagination,
		func(_ uint64, value types.VaultProposal) (types.VaultProposal, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllVaultProposalResponse{VaultProposal: vaultProposals, Pagination: pageRes}, nil
}

func (q queryServer) GetVaultProposal(ctx context.Context, req *types.QueryGetVaultProposalRequest) (*types.QueryGetVaultProposalResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.VaultProposal.Get(ctx, req.ProposalId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetVaultProposalResponse{VaultProposal: val}, nil
}
