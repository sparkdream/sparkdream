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

func (q queryServer) ListGuildInvite(ctx context.Context, req *types.QueryAllGuildInviteRequest) (*types.QueryAllGuildInviteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	guildInvites, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.GuildInvite,
		req.Pagination,
		func(_ string, value types.GuildInvite) (types.GuildInvite, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllGuildInviteResponse{GuildInvite: guildInvites, Pagination: pageRes}, nil
}

func (q queryServer) GetGuildInvite(ctx context.Context, req *types.QueryGetGuildInviteRequest) (*types.QueryGetGuildInviteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.GuildInvite.Get(ctx, req.GuildInvitee)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetGuildInviteResponse{GuildInvite: val}, nil
}
