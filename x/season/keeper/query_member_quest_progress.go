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

func (q queryServer) ListMemberQuestProgress(ctx context.Context, req *types.QueryAllMemberQuestProgressRequest) (*types.QueryAllMemberQuestProgressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	memberQuestProgresss, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.MemberQuestProgress,
		req.Pagination,
		func(_ string, value types.MemberQuestProgress) (types.MemberQuestProgress, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllMemberQuestProgressResponse{MemberQuestProgress: memberQuestProgresss, Pagination: pageRes}, nil
}

func (q queryServer) GetMemberQuestProgress(ctx context.Context, req *types.QueryGetMemberQuestProgressRequest) (*types.QueryGetMemberQuestProgressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.MemberQuestProgress.Get(ctx, req.MemberQuest)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetMemberQuestProgressResponse{MemberQuestProgress: val}, nil
}
