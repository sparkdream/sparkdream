package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ProjectsByCreator(ctx context.Context, req *types.QueryProjectsByCreatorRequest) (*types.QueryProjectsByCreatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Creator == "" {
		return nil, status.Error(codes.InvalidArgument, "creator cannot be empty")
	}

	projects, err := q.collectProjects(ctx, func(p types.Project) bool {
		return p.Creator == req.Creator
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	reverse := req.Pagination != nil && req.Pagination.Reverse
	if err := sortProjects(projects, req.SortBy, reverse); err != nil {
		return nil, err
	}
	page, pageRes, err := paginateSorted(projects, req.Pagination)
	if err != nil {
		return nil, err
	}

	return &types.QueryProjectsByCreatorResponse{
		Projects:   page,
		Pagination: pageRes,
	}, nil
}
