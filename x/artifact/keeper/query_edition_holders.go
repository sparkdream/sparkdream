package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) EditionHolders(ctx context.Context, req *types.QueryEditionHoldersRequest) (*types.QueryEditionHoldersResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	e, err := q.k.Editions.Get(ctx, collections.Join(req.ClassId, req.EditionId))
	if err != nil {
		return nil, notFound(err, "edition")
	}
	balances, page, err := query.CollectionPaginate(ctx, q.k.Balances, capPage(req.Pagination),
		func(_ BalanceKey, b types.Balance) (types.BalanceView, error) { return balanceView(e, b), nil },
		editionPrefix[string](req.ClassId, req.EditionId))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryEditionHoldersResponse{Balances: balances, Pagination: page}, nil
}
