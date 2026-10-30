package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"

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

	// A member without a profile cannot start quests (MsgStartQuest
	// refuses), so nothing is available.
	profile, err := q.k.MemberProfile.Get(ctx, req.Member)
	if err != nil {
		return &types.QueryAvailableQuestsResponse{}, nil
	}
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// SEA-35: every quest the member could start now, paginated over the
	// quest table, judged by the same predicate as MsgStartQuest.
	quests, pageRes, err := query.CollectionFilteredPaginate(ctx, q.k.Quest, req.Pagination,
		func(_ string, quest types.Quest) (bool, error) {
			return q.k.canStartQuest(ctx, req.Member, profile, quest, params.MaxActiveQuestsPerMember) == nil, nil
		},
		func(_ string, quest types.Quest) (types.Quest, error) { return quest, nil },
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryAvailableQuestsResponse{Quests: quests, Pagination: pageRes}, nil
}
