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

func (q queryServer) ListVault(ctx context.Context, req *types.QueryAllVaultRequest) (*types.QueryAllVaultResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	vaults, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Vault,
		req.Pagination,
		func(_ uint64, value types.Vault) (types.Vault, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllVaultResponse{Vault: vaults, Pagination: pageRes}, nil
}

func (q queryServer) GetVault(ctx context.Context, req *types.QueryGetVaultRequest) (*types.QueryGetVaultResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.Vault.Get(ctx, req.VaultId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetVaultResponse{Vault: val}, nil
}
