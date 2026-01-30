package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListBounty(ctx context.Context, req *types.QueryAllBountyRequest) (*types.QueryAllBountyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	bountys, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Bounty,
		req.Pagination,
		func(_ uint64, value types.Bounty) (types.Bounty, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllBountyResponse{Bounty: bountys, Pagination: pageRes}, nil
}

func (q queryServer) GetBounty(ctx context.Context, req *types.QueryGetBountyRequest) (*types.QueryGetBountyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	bounty, err := q.k.Bounty.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetBountyResponse{Bounty: bounty}, nil
}
