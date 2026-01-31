package keeper

import (
	"context"
	"strconv"
	"strings"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MemberGuildInvites(ctx context.Context, req *types.QueryMemberGuildInvitesRequest) (*types.QueryMemberGuildInvitesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Member == "" {
		return nil, status.Error(codes.InvalidArgument, "member address required")
	}

	// Iterate through invites to find those for this member
	// Keys are formatted as "guildId:invitee"
	iter, err := q.k.GuildInvite.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	var foundGuildId uint64
	var foundGuildName string

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			continue
		}
		invite, err := iter.Value()
		if err != nil {
			continue
		}
		if invite.GuildInvitee == req.Member {
			// Extract guild ID from key (format: "guildId:invitee")
			guildId := extractGuildIdFromInviteKey(key)
			if guildId > 0 {
				foundGuildId = guildId
				// Get guild name
				guild, err := q.k.Guild.Get(ctx, guildId)
				if err == nil {
					foundGuildName = guild.Name
				}
			}
			break // Return first match for non-paginated response
		}
	}

	return &types.QueryMemberGuildInvitesResponse{
		GuildId:   foundGuildId,
		GuildName: foundGuildName,
	}, nil
}

// Helper to extract guild ID from invite key (format: "guildId:invitee")
func extractGuildIdFromInviteKey(key string) uint64 {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) >= 1 {
		guildId, err := strconv.ParseUint(parts[0], 10, 64)
		if err == nil {
			return guildId
		}
	}
	return 0
}
