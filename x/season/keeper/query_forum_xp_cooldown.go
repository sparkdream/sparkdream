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

func (q queryServer) ListForumXpCooldown(ctx context.Context, req *types.QueryAllForumXpCooldownRequest) (*types.QueryAllForumXpCooldownResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	forumXpCooldowns, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.ForumXpCooldown,
		req.Pagination,
		func(_ string, value types.ForumXpCooldown) (types.ForumXpCooldown, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllForumXpCooldownResponse{ForumXpCooldown: forumXpCooldowns, Pagination: pageRes}, nil
}

func (q queryServer) GetForumXpCooldown(ctx context.Context, req *types.QueryGetForumXpCooldownRequest) (*types.QueryGetForumXpCooldownResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.ForumXpCooldown.Get(ctx, req.BeneficiaryActor)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetForumXpCooldownResponse{ForumXpCooldown: val}, nil
}
