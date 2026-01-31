package keeper

import (
	"context"
	"fmt"
	"strings"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GuildInvites(ctx context.Context, req *types.QueryGuildInvitesRequest) (*types.QueryGuildInvitesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Verify guild exists
	_, err := q.k.Guild.Get(ctx, req.GuildId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "guild %d not found", req.GuildId)
	}

	// Iterate through invites to find those for this guild
	// Keys are formatted as "guildId:invitee"
	prefix := fmt.Sprintf("%d:", req.GuildId)

	iter, err := q.k.GuildInvite.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	var foundInvitee string
	var foundInviter string
	var foundInvitedEpoch int64

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			continue
		}
		if strings.HasPrefix(key, prefix) {
			invite, err := iter.Value()
			if err != nil {
				continue
			}
			foundInvitee = invite.GuildInvitee
			foundInviter = invite.Inviter
			foundInvitedEpoch = invite.ExpiresEpoch
			break // Return first match for non-paginated response
		}
	}

	return &types.QueryGuildInvitesResponse{
		Invitee:      foundInvitee,
		Inviter:      foundInviter,
		ExpiresEpoch: foundInvitedEpoch, // Using InvitedEpoch as expires, real expiry would add invite duration
	}, nil
}
