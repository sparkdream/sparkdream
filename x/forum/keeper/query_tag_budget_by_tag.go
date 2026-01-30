package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) TagBudgetByTag(ctx context.Context, req *types.QueryTagBudgetByTagRequest) (*types.QueryTagBudgetByTagResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Tag == "" {
		return nil, status.Error(codes.InvalidArgument, "tag required")
	}

	// Find tag budget for this tag
	var foundBudget *types.TagBudget

	err := q.k.TagBudget.Walk(ctx, nil, func(key uint64, budget types.TagBudget) (bool, error) {
		if budget.Tag == req.Tag {
			foundBudget = &budget
			return true, nil // Stop after first
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundBudget != nil {
		return &types.QueryTagBudgetByTagResponse{
			BudgetId:    foundBudget.Id,
			PoolBalance: foundBudget.PoolBalance,
			Active:      foundBudget.Active,
		}, nil
	}

	return &types.QueryTagBudgetByTagResponse{}, nil
}
