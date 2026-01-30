package keeper

import (
	"context"
	"errors"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListSeasonSnapshot(ctx context.Context, req *types.QueryAllSeasonSnapshotRequest) (*types.QueryAllSeasonSnapshotResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	seasonSnapshots, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.SeasonSnapshot,
		req.Pagination,
		func(_ uint64, value types.SeasonSnapshot) (types.SeasonSnapshot, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllSeasonSnapshotResponse{SeasonSnapshot: seasonSnapshots, Pagination: pageRes}, nil
}

func (q queryServer) GetSeasonSnapshot(ctx context.Context, req *types.QueryGetSeasonSnapshotRequest) (*types.QueryGetSeasonSnapshotResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.SeasonSnapshot.Get(ctx, req.Season)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetSeasonSnapshotResponse{SeasonSnapshot: val}, nil
}
