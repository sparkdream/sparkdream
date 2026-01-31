package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GuildsByFounder(ctx context.Context, req *types.QueryGuildsByFounderRequest) (*types.QueryGuildsByFounderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Founder == "" {
		return nil, status.Error(codes.InvalidArgument, "founder address required")
	}

	// Iterate through guilds and find those founded by the address
	iter, err := q.k.Guild.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	var foundGuildId uint64
	var foundGuild *types.Guild

	for ; iter.Valid(); iter.Next() {
		guild, err := iter.Value()
		if err != nil {
			continue
		}
		if guild.Founder == req.Founder {
			guildId, _ := iter.Key()
			foundGuildId = guildId
			foundGuild = &guild
			break // Return first match for non-paginated response
		}
	}

	if foundGuild == nil {
		return &types.QueryGuildsByFounderResponse{}, nil
	}

	return &types.QueryGuildsByFounderResponse{
		Id:     foundGuildId,
		Name:   foundGuild.Name,
		Status: uint64(foundGuild.Status),
	}, nil
}
