package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GuildById(ctx context.Context, req *types.QueryGuildByIdRequest) (*types.QueryGuildByIdResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	guild, err := q.k.Guild.Get(ctx, req.GuildId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "guild %d not found", req.GuildId)
	}

	return &types.QueryGuildByIdResponse{
		Name:        guild.Name,
		Description: guild.Description,
		Founder:     guild.Founder,
		InviteOnly:  guild.InviteOnly,
		Status:      uint64(guild.Status),
	}, nil
}
