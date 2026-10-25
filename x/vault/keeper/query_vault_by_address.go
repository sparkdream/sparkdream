package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vault/types"
)

func (q queryServer) VaultByAddress(ctx context.Context, req *types.QueryVaultByAddressRequest) (*types.QueryVaultByAddressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	id, err := q.k.VaultByAddress.Get(ctx, req.Address)
	if err != nil {
		return nil, status.Error(codes.NotFound, "not found")
	}
	v, err := q.k.Vault.Get(ctx, id)
	if err != nil {
		return nil, status.Error(codes.NotFound, "not found")
	}
	return &types.QueryVaultByAddressResponse{Vault: v}, nil
}
