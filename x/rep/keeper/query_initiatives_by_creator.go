package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) InitiativesByCreator(ctx context.Context, req *types.QueryInitiativesByCreatorRequest) (*types.QueryInitiativesByCreatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Creator == "" {
		return nil, status.Error(codes.InvalidArgument, "creator cannot be empty")
	}

	initiatives, err := q.collectInitiatives(ctx, func(ini types.Initiative) bool {
		return ini.Creator == req.Creator
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

	return &types.QueryInitiativesByCreatorResponse{
		Initiatives: page,
		Pagination:  pageRes,
	}, nil
}
