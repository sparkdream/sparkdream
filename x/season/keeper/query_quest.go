package keeper

import (
	"context"
	"errors"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListQuest(ctx context.Context, req *types.QueryAllQuestRequest) (*types.QueryAllQuestResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	quests, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Quest,
		req.Pagination,
		func(_ string, value types.Quest) (types.Quest, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllQuestResponse{Quest: quests, Pagination: pageRes}, nil
}

func (q queryServer) GetQuest(ctx context.Context, req *types.QueryGetQuestRequest) (*types.QueryGetQuestResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.Quest.Get(ctx, req.QuestId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetQuestResponse{Quest: val}, nil
}
