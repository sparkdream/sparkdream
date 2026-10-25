package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) TokensByOwner(ctx context.Context, req *types.QueryTokensByOwnerRequest) (*types.QueryTokensByOwnerResponse, error) {
	if req == nil || req.Owner == "" {
		return nil, errInvalidRequest
	}
	addrs := q.k.allAddressesOf(ctx, req.Owner)
	prefix := addrPrefix(addrs[0])
	if req.ClassId != 0 {
		prefix = addrClassPrefix(addrs[0], req.ClassId)
	}
	cache := map[uint64]types.ContentStatus{}
	view := func(key AddrTokenKey, _ collections.NoValue) (types.TokenView, error) {
		t, err := q.k.Tokens.Get(ctx, collections.Join(key.K2(), key.K3()))
		if err != nil {
			return types.TokenView{}, err
		}
		return TokenView(ctx, q.classStatus(ctx, cache, t.ClassId), t, q.k.treeWithheld(ctx, t)), nil
	}
	views, page, err := query.CollectionPaginate(ctx, q.k.TokensByOwner, capPage(req.Pagination), view, prefix)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if lastPage(page) {
		for _, a := range addrs[1:] {
			rng := addrRange(a, req.ClassId)
			err = q.k.TokensByOwner.Walk(ctx, rng, func(key AddrTokenKey) (bool, error) {
				v, err := view(key, collections.NoValue{})
				if err != nil {
					return true, err
				}
				views = append(views, v)
				return false, nil
			})
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
	}
	return &types.QueryTokensByOwnerResponse{Tokens: views, Pagination: page}, nil
}
