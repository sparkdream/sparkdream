package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) AvailableInitiatives(ctx context.Context, req *types.QueryAvailableInitiativesRequest) (*types.QueryAvailableInitiativesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Available = status OPEN: exactly the set that can still be assigned.
	initiatives, err := q.collectInitiatives(ctx, func(ini types.Initiative) bool {
		return ini.Status == types.InitiativeStatus_INITIATIVE_STATUS_OPEN
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

	return &types.QueryAvailableInitiativesResponse{
		Initiatives: page,
		Pagination:  pageRes,
	}, nil
}
