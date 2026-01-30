package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Categories(ctx context.Context, req *types.QueryCategoriesRequest) (*types.QueryCategoriesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Get first category (simplified - in production would return list with pagination)
	var firstCategory *types.Category

	err := q.k.Category.Walk(ctx, nil, func(key uint64, category types.Category) (bool, error) {
		firstCategory = &category
		return true, nil // Stop after first
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if firstCategory != nil {
		return &types.QueryCategoriesResponse{
			CategoryId: firstCategory.CategoryId,
			Title:      firstCategory.Title,
		}, nil
	}

	return &types.QueryCategoriesResponse{}, nil
}
