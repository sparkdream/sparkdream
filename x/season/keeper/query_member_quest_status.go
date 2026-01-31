package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MemberQuestStatus(ctx context.Context, req *types.QueryMemberQuestStatusRequest) (*types.QueryMemberQuestStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Member == "" {
		return nil, status.Error(codes.InvalidArgument, "member address required")
	}

	if req.QuestId == "" {
		return nil, status.Error(codes.InvalidArgument, "quest_id required")
	}

	// Verify quest exists
	_, err := q.k.Quest.Get(ctx, req.QuestId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "quest %s not found", req.QuestId)
	}

	// Get member's progress on this quest
	progressKey := fmt.Sprintf("%s:%s", req.Member, req.QuestId)
	progress, err := q.k.MemberQuestProgress.Get(ctx, progressKey)
	if err != nil {
		// No progress record means not started/not completed
		return &types.QueryMemberQuestStatusResponse{
			Completed:      false,
			CompletedBlock: 0,
		}, nil
	}

	return &types.QueryMemberQuestStatusResponse{
		Completed:      progress.Completed,
		CompletedBlock: progress.CompletedBlock,
	}, nil
}
