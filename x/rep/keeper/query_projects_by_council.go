package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ProjectsByCouncil(ctx context.Context, req *types.QueryProjectsByCouncilRequest) (*types.QueryProjectsByCouncilResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Council == "" {
		return nil, status.Error(codes.InvalidArgument, "council cannot be empty")
	}

	projects, err := q.collectProjects(ctx, func(p types.Project) bool {
		return p.Council == req.Council
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

	return &types.QueryProjectsByCouncilResponse{
		Projects:   page,
		Pagination: pageRes,
	}, nil
}
