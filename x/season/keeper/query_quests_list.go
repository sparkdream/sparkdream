package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) QuestsList(ctx context.Context, req *types.QueryQuestsListRequest) (*types.QueryQuestsListResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Use collection query for pagination
	quests, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Quest,
		req.Pagination,
		func(key string, quest types.Quest) (types.Quest, error) {
			return quest, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if len(quests) == 0 {
		return &types.QueryQuestsListResponse{
			Pagination: pageRes,
		}, nil
	}

	firstQuest := quests[0]
	return &types.QueryQuestsListResponse{
		Id:         firstQuest.QuestId,
		Name:       firstQuest.Name,
		XpReward:   firstQuest.XpReward,
		Active:     firstQuest.Active,
		Pagination: pageRes,
	}, nil
}
