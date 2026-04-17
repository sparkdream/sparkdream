package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) TagBudgets(ctx context.Context, req *types.QueryTagBudgetsRequest) (*types.QueryTagBudgetsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Summary query: returns the first tag budget found. Matches the legacy
	// x/forum query shape so callers migrating from forum endpoints see the
	// same response structure.
	var firstBudget *types.TagBudget

	err := q.k.TagBudget.Walk(ctx, nil, func(_ uint64, budget types.TagBudget) (bool, error) {
		firstBudget = &budget
		return true, nil
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
