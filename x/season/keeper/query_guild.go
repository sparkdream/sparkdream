package keeper

import (
	"context"
	"errors"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListGuild(ctx context.Context, req *types.QueryAllGuildRequest) (*types.QueryAllGuildResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	guilds, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Guild,
		req.Pagination,
		func(_ uint64, value types.Guild) (types.Guild, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllGuildResponse{Guild: guilds, Pagination: pageRes}, nil
}

func (q queryServer) GetGuild(ctx context.Context, req *types.QueryGetGuildRequest) (*types.QueryGetGuildResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	guild, err := q.k.Guild.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetGuildResponse{Guild: guild}, nil
}
