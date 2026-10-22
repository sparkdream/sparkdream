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
	balances, page, err := query.CollectionPaginate(ctx, q.k.BalancesByOwner, capPage(req.Pagination),
		func(key AddrTokenKey, _ collections.NoValue) (types.BalanceView, error) {
			b, err := q.k.Balances.Get(ctx, balanceKey(key.K2(), key.K3(), key.K1()))
			if err != nil {
				return types.BalanceView{}, err
			}
			return balanceView(q.editionCached(ctx, cache, key.K2(), key.K3()), b), nil
		}, addrPrefix(req.Owner))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryBalancesByOwnerResponse{Balances: balances, Pagination: page}, nil
}
