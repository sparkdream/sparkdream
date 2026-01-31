package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) QuestById(ctx context.Context, req *types.QueryQuestByIdRequest) (*types.QueryQuestByIdResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.QuestId == "" {
		return nil, status.Error(codes.InvalidArgument, "quest_id required")
	}

	quest, err := q.k.Quest.Get(ctx, req.QuestId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "quest %s not found", req.QuestId)
	}

	return &types.QueryQuestByIdResponse{
		Name:        quest.Name,
		Description: quest.Description,
		XpReward:    quest.XpReward,
		Active:      quest.Active,
		MinLevel:    quest.MinLevel,
	}, nil
}
