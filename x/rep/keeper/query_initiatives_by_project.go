package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) InitiativesByProject(ctx context.Context, req *types.QueryInitiativesByProjectRequest) (*types.QueryInitiativesByProjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	initiatives, err := q.collectInitiatives(ctx, func(ini types.Initiative) bool {
		return ini.ProjectId == req.ProjectId
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	reverse := req.Pagination != nil && req.Pagination.Reverse
	if err := sortInitiatives(initiatives, req.SortBy, reverse); err != nil {
		return nil, err
	}
	page, pageRes, err := paginateSorted(initiatives, req.Pagination)
	if err != nil {
		return nil, err
	}

	// The response predates the repeated-value convention and declares
	// pointers; earlier revisions also ignored pagination entirely, so
	// honoring it here only trims what clients previously got in one shot.
	out := make([]*types.Initiative, len(page))
	for i := range page {
		out[i] = &page[i]
	}
	return &types.QueryInitiativesByProjectResponse{
		Initiatives: out,
		Pagination:  pageRes,
	}, nil
}
