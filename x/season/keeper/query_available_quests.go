package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) AvailableQuests(ctx context.Context, req *types.QueryAvailableQuestsRequest) (*types.QueryAvailableQuestsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Member == "" {
		return nil, status.Error(codes.InvalidArgument, "member address required")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	// Get member's profile to check level
	profile, err := q.k.MemberProfile.Get(ctx, req.Member)
	memberLevel := uint64(1)
	if err == nil {
		memberLevel = q.k.CalculateLevel(ctx, profile.SeasonXp)
	}

	// Iterate through quests to find available ones
	iter, err := q.k.Quest.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	var foundQuestId string
	var foundQuestName string
	var foundXpReward uint64

	for ; iter.Valid(); iter.Next() {
		quest, err := iter.Value()
		if err != nil {
			continue
		}

		// Check if quest is active
		if !quest.Active {
			continue
		}

		// Check if quest is within time window
		if quest.StartBlock > 0 && currentBlock < quest.StartBlock {
			continue
		}
		if quest.EndBlock > 0 && currentBlock > quest.EndBlock {
			continue
		}

		// Check minimum level requirement
		if quest.MinLevel > memberLevel {
			continue
		}

		// Check if member has already completed this quest (if non-repeatable)
		progressKey := fmt.Sprintf("%s:%s", req.Member, quest.QuestId)
		progress, progressErr := q.k.MemberQuestProgress.Get(ctx, progressKey)
		if progressErr == nil && progress.Completed && !quest.Repeatable {
			continue
		}

		// Check prerequisite quest
		if quest.PrerequisiteQuest != "" {
			prereqKey := fmt.Sprintf("%s:%s", req.Member, quest.PrerequisiteQuest)
			prereqProgress, prereqErr := q.k.MemberQuestProgress.Get(ctx, prereqKey)
			if prereqErr != nil || !prereqProgress.Completed {
				continue
			}
		}

		// Found an available quest
		foundQuestId = quest.QuestId
		foundQuestName = quest.Name
		foundXpReward = quest.XpReward
		break
	}

	return &types.QueryAvailableQuestsResponse{
		Id:       foundQuestId,
		Name:     foundQuestName,
		XpReward: foundXpReward,
	}, nil
}
