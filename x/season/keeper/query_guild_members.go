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

	// Iterate through memberships to find members of this guild
	iter, err := q.k.GuildMembership.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	var foundMember string
	var foundJoinedEpoch int64

	for ; iter.Valid(); iter.Next() {
		membership, err := iter.Value()
		if err != nil {
			continue
		}
		// Only include active members (LeftEpoch == 0 means still active)
		if membership.GuildId == req.GuildId && membership.LeftEpoch == 0 {
			memberAddr, _ := iter.Key()
			foundMember = memberAddr
			foundJoinedEpoch = membership.JoinedEpoch
			break // Return first match for non-paginated response
		}
	}

	return &types.QueryGuildMembersResponse{
		Member:      foundMember,
		JoinedEpoch: foundJoinedEpoch,
	}, nil
}
