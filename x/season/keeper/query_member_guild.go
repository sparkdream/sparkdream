package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MemberGuild(ctx context.Context, req *types.QueryMemberGuildRequest) (*types.QueryMemberGuildResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Member == "" {
		return nil, status.Error(codes.InvalidArgument, "member address required")
	}

	membership, err := q.k.GuildMembership.Get(ctx, req.Member)
	if err != nil {
		// Member not in any guild
		return &types.QueryMemberGuildResponse{
			GuildId:     0,
			JoinedEpoch: 0,
		}, nil
	}

	// Check if membership is still active (LeftEpoch == 0)
	if membership.LeftEpoch != 0 {
		return &types.QueryMemberGuildResponse{
			GuildId:     0,
			JoinedEpoch: 0,
		}, nil
	}

	return &types.QueryMemberGuildResponse{
		GuildId:     membership.GuildId,
		JoinedEpoch: membership.JoinedEpoch,
	}, nil
}
