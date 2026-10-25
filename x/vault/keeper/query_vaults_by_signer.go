package keeper

import (
	"context"
	"sort"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vault/types"
)

// VaultsBySigner lists the vaults the address, or any x/rep alias of it, is a
// signer of. Pagination is offset-based over the merged id list.
func (q queryServer) VaultsBySigner(ctx context.Context, req *types.QueryVaultsBySignerRequest) (*types.QueryVaultsBySignerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if _, err := q.k.addressCodec.StringToBytes(req.Address); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid address")
	}
	seen := map[uint64]bool{}
	var ids []uint64
	for _, addr := range q.k.allAddressesOf(ctx, req.Address) {
		rng := collections.NewPrefixedPairRange[string, uint64](addr)
		if err := q.k.VaultsBySigner.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			if !seen[key.K2()] {
				seen[key.K2()] = true
				ids = append(ids, key.K2())
			}
			return false, nil
		}); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	offset, limit := uint64(0), uint64(query.DefaultLimit)
	if req.Pagination != nil {
		offset = req.Pagination.Offset
		if req.Pagination.Limit > 0 {
			limit = req.Pagination.Limit
		}
	}
	var out []types.Vault
	for i := offset; i < uint64(len(ids)) && uint64(len(out)) < limit; i++ {
		if v, err := q.k.Vault.Get(ctx, ids[i]); err == nil {
			out = append(out, v)
		}
	}
	return &types.QueryVaultsBySignerResponse{Vaults: out, Pagination: &query.PageResponse{Total: uint64(len(ids))}}, nil
}
