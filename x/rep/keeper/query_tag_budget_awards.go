package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) TagBudgetAwards(ctx context.Context, req *types.QueryTagBudgetAwardsRequest) (*types.QueryTagBudgetAwardsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.BudgetId == 0 {
		return nil, status.Error(codes.InvalidArgument, "budget_id required")
	}

	var foundAward *types.TagBudgetAward

	err := q.k.TagBudgetAward.Walk(ctx, nil, func(_ uint64, award types.TagBudgetAward) (bool, error) {
		if award.BudgetId == req.BudgetId {
			foundAward = &award
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundAward != nil {
		return &types.QueryTagBudgetAwardsResponse{
			PostId:    foundAward.PostId,
			Recipient: foundAward.Recipient,
			Amount:    foundAward.Amount,
		}, nil
	}

	return &types.QueryTagBudgetAwardsResponse{}, nil
}
