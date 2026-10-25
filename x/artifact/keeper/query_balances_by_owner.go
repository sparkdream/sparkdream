package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) BalancesByOwner(ctx context.Context, req *types.QueryBalancesByOwnerRequest) (*types.QueryBalancesByOwnerResponse, error) {
	if req == nil || req.Owner == "" {
		return nil, errInvalidRequest
	}
	cache := map[[2]uint64]types.Edition{}
	addrs := q.k.allAddressesOf(ctx, req.Owner)
	view := func(key AddrTokenKey, _ collections.NoValue) (types.BalanceView, error) {
		b, err := q.k.Balances.Get(ctx, balanceKey(key.K2(), key.K3(), key.K1()))
		if err != nil {
			return types.BalanceView{}, err
		}
		return balanceView(q.editionCached(ctx, cache, key.K2(), key.K3()), b), nil
	}
	balances, page, err := query.CollectionPaginate(ctx, q.k.BalancesByOwner, capPage(req.Pagination), view, addrPrefix(addrs[0]))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if lastPage(page) {
		for _, a := range addrs[1:] {
			err = q.k.BalancesByOwner.Walk(ctx, addrRange(a, 0), func(key AddrTokenKey) (bool, error) {
				v, err := view(key, collections.NoValue{})
				if err != nil {
					return true, err
				}
				balances = append(balances, v)
				return false, nil
			})
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
	}
	return &types.QueryBalancesByOwnerResponse{Balances: balances, Pagination: page}, nil
}
