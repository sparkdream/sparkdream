package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/commons/types"
)

// GetRecurringSpend returns a single schedule by id. Errors with
// codes.NotFound if no such schedule exists, mirroring GetCategory.
func (q queryServer) GetRecurringSpend(ctx context.Context, req *types.QueryGetRecurringSpendRequest) (*types.QueryGetRecurringSpendResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	rs, err := q.k.RecurringSpends.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "recurring spend not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryGetRecurringSpendResponse{RecurringSpend: rs}, nil
}

// ListRecurringSpends paginates schedules. If `authority` or `recipient`
// is set, results are restricted to the matching index; otherwise the
// full collection is walked.
//
// Specifying both authority and recipient at once is rejected — neither
// index would obviously win, and the answer rarely matches a real query.
// Callers that want the intersection can filter client-side.
func (q queryServer) ListRecurringSpends(ctx context.Context, req *types.QueryListRecurringSpendsRequest) (*types.QueryListRecurringSpendsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Authority != "" && req.Recipient != "" {
		return nil, status.Error(codes.InvalidArgument, "specify at most one of authority or recipient")
	}

	switch {
	case req.Authority != "":
		return q.listByIndex(ctx, q.k.RecurringSpendsByAuthority, req.Authority, req.Pagination)
	case req.Recipient != "":
		return q.listByIndex(ctx, q.k.RecurringSpendsByRecipient, req.Recipient, req.Pagination)
	}

	out, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.RecurringSpends,
		req.Pagination,
		func(_ uint64, v types.RecurringSpend) (types.RecurringSpend, error) {
			return v, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryListRecurringSpendsResponse{RecurringSpends: out, Pagination: pageRes}, nil
}

// listByIndex paginates one of the (key, id) keysets and hydrates each id
// back into a full RecurringSpend. Pagination keys remain on the index,
// not on the value map, so this stays consistent across schedule edits.
func (q queryServer) listByIndex(
	ctx context.Context,
	idx collections.KeySet[collections.Pair[string, uint64]],
	key string,
	pageReq *query.PageRequest,
) (*types.QueryListRecurringSpendsResponse, error) {
	rng := collections.NewPrefixedPairRange[string, uint64](key)
	pairs, pageRes, err := query.CollectionPaginate(
		ctx,
		idx,
		pageReq,
		func(k collections.Pair[string, uint64], _ collections.NoValue) (uint64, error) {
			return k.K2(), nil
		},
		query.WithCollectionPaginationPairPrefix[string, uint64](key),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	_ = rng

	out := make([]types.RecurringSpend, 0, len(pairs))
	for _, id := range pairs {
		rs, err := q.k.RecurringSpends.Get(ctx, id)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		out = append(out, rs)
	}
	return &types.QueryListRecurringSpendsResponse{RecurringSpends: out, Pagination: pageRes}, nil
}
