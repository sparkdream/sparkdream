package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GuildsList(ctx context.Context, req *types.QueryGuildsListRequest) (*types.QueryGuildsListResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Use collection query for pagination
	guilds, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Guild,
		req.Pagination,
		func(key uint64, guild types.Guild) (types.Guild, error) {
			return guild, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Return the first guild if available (response format expects single guild with pagination)
	if len(guilds) == 0 {
		return &types.QueryGuildsListResponse{
			Pagination: pageRes,
		}, nil
	}

	// Get the key for the first guild
	var firstGuildId uint64
	iter, err := q.k.Guild.Iterate(ctx, nil)
	if err == nil {
		defer iter.Close()
		if iter.Valid() {
			firstGuildId, _ = iter.Key()
		}
	}

	firstGuild := guilds[0]
	return &types.QueryGuildsListResponse{
		Id:         firstGuildId,
		Name:       firstGuild.Name,
		Founder:    firstGuild.Founder,
		Status:     uint64(firstGuild.Status),
		Pagination: pageRes,
	}, nil
}
