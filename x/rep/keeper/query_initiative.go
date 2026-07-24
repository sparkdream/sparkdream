package keeper

import (
	"context"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListInitiative(ctx context.Context, req *types.QueryAllInitiativeRequest) (*types.QueryAllInitiativeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// A sorted list can't page by store key (the sort order isn't the store
	// order), so sort_by switches to collect-sort-offset pagination. The
	// key-paginated path stays as-is for unsorted queries.
	if req.SortBy != "" {
		all, err := q.collectInitiatives(ctx, nil)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		reverse := req.Pagination != nil && req.Pagination.Reverse
		if err := sortInitiatives(all, req.SortBy, reverse); err != nil {
			return nil, err
		}
		page, pageRes, err := paginateSorted(all, req.Pagination)
		if err != nil {
			return nil, err
		}
		return &types.QueryAllInitiativeResponse{Initiative: page, Pagination: pageRes}, nil
	}

	initiatives, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Initiative,
		req.Pagination,
		func(_ uint64, value types.Initiative) (types.Initiative, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllInitiativeResponse{Initiative: initiatives, Pagination: pageRes}, nil
}

func (q queryServer) GetInitiative(ctx context.Context, req *types.QueryGetInitiativeRequest) (*types.QueryGetInitiativeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	initiative, err := q.k.Initiative.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetInitiativeResponse{Initiative: initiative}, nil
}
