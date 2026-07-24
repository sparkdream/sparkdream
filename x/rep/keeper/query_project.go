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

func (q queryServer) ListProject(ctx context.Context, req *types.QueryAllProjectRequest) (*types.QueryAllProjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// A sorted list can't page by store key (the sort order isn't the store
	// order), so sort_by switches to collect-sort-offset pagination. The
	// key-paginated path stays as-is for unsorted queries.
	if req.SortBy != "" {
		all, err := q.collectProjects(ctx, nil)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		reverse := req.Pagination != nil && req.Pagination.Reverse
		if err := sortProjects(all, req.SortBy, reverse); err != nil {
			return nil, err
		}
		page, pageRes, err := paginateSorted(all, req.Pagination)
		if err != nil {
			return nil, err
		}
		return &types.QueryAllProjectResponse{Project: page, Pagination: pageRes}, nil
	}

	projects, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Project,
		req.Pagination,
		func(_ uint64, value types.Project) (types.Project, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllProjectResponse{Project: projects, Pagination: pageRes}, nil
}

func (q queryServer) GetProject(ctx context.Context, req *types.QueryGetProjectRequest) (*types.QueryGetProjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	project, err := q.k.Project.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetProjectResponse{Project: project}, nil
}
