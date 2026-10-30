package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GuildMembers(ctx context.Context, req *types.QueryGuildMembersRequest) (*types.QueryGuildMembersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Verify guild exists
	_, err := q.k.Guild.Get(ctx, req.GuildId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "guild %d not found", req.GuildId)
	}

	// First active member, through the ByGuild index (SEA-28).
	var foundMember string
	var foundJoinedEpoch int64
	if members := q.k.guildMembers(ctx, req.GuildId); len(members) > 0 {
		foundMember = members[0]
		if m, err := q.k.GuildMembership.Get(ctx, foundMember); err == nil {
			foundJoinedEpoch = m.JoinedEpoch
		}
	}

	return &types.QueryGuildMembersResponse{
		Member:      foundMember,
		JoinedEpoch: foundJoinedEpoch,
	}, nil
}
