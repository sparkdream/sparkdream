package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) TagBudgets(ctx context.Context, req *types.QueryTagBudgetsRequest) (*types.QueryTagBudgetsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Get first tag budget (simplified - in production would return list with pagination)
	var firstBudget *types.TagBudget

	err := q.k.TagBudget.Walk(ctx, nil, func(key uint64, budget types.TagBudget) (bool, error) {
		firstBudget = &budget
		return true, nil // Stop after first
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if firstBudget != nil {
		return &types.QueryTagBudgetsResponse{
			BudgetId:    firstBudget.Id,
			Tag:         firstBudget.Tag,
			PoolBalance: firstBudget.PoolBalance,
		}, nil
	}

	return &types.QueryTagBudgetsResponse{}, nil
}
