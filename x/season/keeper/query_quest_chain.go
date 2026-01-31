package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) QuestChain(ctx context.Context, req *types.QueryQuestChainRequest) (*types.QueryQuestChainResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.QuestChain == "" {
		return nil, status.Error(codes.InvalidArgument, "quest_chain required")
	}

	// Iterate through quests to find those in the chain
	iter, err := q.k.Quest.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	var chainQuests []string
	for ; iter.Valid(); iter.Next() {
		quest, err := iter.Value()
		if err != nil {
			continue
		}
		// If quest belongs to this chain
		if quest.QuestChain == req.QuestChain {
			chainQuests = append(chainQuests, quest.QuestId)
		}
	}

	if len(chainQuests) == 0 {
		return &types.QueryQuestChainResponse{}, nil
	}

	// Return the first quest in chain as representative
	firstQuest, _ := q.k.Quest.Get(ctx, chainQuests[0])
	return &types.QueryQuestChainResponse{
		QuestId:           chainQuests[0],
		Name:              firstQuest.Name,
		PrerequisiteQuest: firstQuest.PrerequisiteQuest,
	}, nil
}
