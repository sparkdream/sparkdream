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

func (q queryServer) ListGuildMembership(ctx context.Context, req *types.QueryAllGuildMembershipRequest) (*types.QueryAllGuildMembershipResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	guildMemberships, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.GuildMembership,
		req.Pagination,
		func(_ string, value types.GuildMembership) (types.GuildMembership, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllGuildMembershipResponse{GuildMembership: guildMemberships, Pagination: pageRes}, nil
}

func (q queryServer) GetGuildMembership(ctx context.Context, req *types.QueryGetGuildMembershipRequest) (*types.QueryGetGuildMembershipResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.GuildMembership.Get(ctx, req.Member)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetGuildMembershipResponse{GuildMembership: val}, nil
}
