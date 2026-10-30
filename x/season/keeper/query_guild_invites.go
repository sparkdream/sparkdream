package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GuildInvites(ctx context.Context, req *types.QueryGuildInvitesRequest) (*types.QueryGuildInvitesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Verify guild exists
	guild, err := q.k.Guild.Get(ctx, req.GuildId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "guild %d not found", req.GuildId)
	}

	// First live invite of the guild, read through its pending list rather
	// than a scan of every invite (SEA-28).
	var foundInvitee string
	var foundInviter string
	var foundInvitedEpoch int64
	for _, invitee := range guild.PendingInvites {
		invite, err := q.k.GuildInvite.Get(ctx, guildInviteKey(req.GuildId, invitee))
		if err != nil {
			continue
		}
		foundInvitee = invite.GuildInvitee
		foundInviter = invite.Inviter
		foundInvitedEpoch = invite.ExpiresEpoch
		break // Return first match for non-paginated response
	}

	return &types.QueryGuildInvitesResponse{
		Invitee:      foundInvitee,
		Inviter:      foundInviter,
		ExpiresEpoch: foundInvitedEpoch, // Using InvitedEpoch as expires, real expiry would add invite duration
	}, nil
}
