package keeper

import (
	"context"
	"strconv"
	"strings"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"

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

	// First invite of this member, through the ByInvitee index (SEA-28).
	var foundGuildId uint64
	var foundGuildName string
	if err := q.k.GuildInvite.Indexes.ByInvitee.Walk(ctx,
		collections.NewPrefixedPairRange[string, string](req.Member),
		func(_ string, key string) (bool, error) {
			foundGuildId = extractGuildIdFromInviteKey(key)
			if guild, err := q.k.Guild.Get(ctx, foundGuildId); err == nil {
				foundGuildName = guild.Name
			}
			return true, nil
		}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
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
