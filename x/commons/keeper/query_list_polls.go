package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListPolls lists stored polls in ID order (pagination.reverse for newest
// first), optionally for one group and one status.
func (q queryServer) ListPolls(ctx context.Context, req *types.QueryListPollsRequest) (*types.QueryListPollsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	keep := func(p types.Poll) bool {
		return req.Status == types.PollStatus_POLL_STATUS_UNSPECIFIED || p.Status == req.Status
	}

	if req.CouncilName != "" {
		polls, pageRes, err := query.CollectionFilteredPaginate(ctx, q.k.PollsByCouncil, req.Pagination,
			func(key collections.Pair[string, uint64], _ collections.NoValue) (bool, error) {
				p, err := q.k.Polls.Get(ctx, key.K2())
				return err == nil && keep(p), nil
			},
			func(key collections.Pair[string, uint64], _ collections.NoValue) (types.Poll, error) {
				return q.k.Polls.Get(ctx, key.K2())
			},
			query.WithCollectionPaginationPairPrefix[string, uint64](req.CouncilName),
		)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &types.QueryListPollsResponse{Polls: polls, Pagination: pageRes}, nil
	}

	polls, pageRes, err := query.CollectionFilteredPaginate(ctx, q.k.Polls, req.Pagination,
		func(_ uint64, p types.Poll) (bool, error) { return keep(p), nil },
		func(_ uint64, p types.Poll) (types.Poll, error) { return p, nil },
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryListPollsResponse{Polls: polls, Pagination: pageRes}, nil
}
