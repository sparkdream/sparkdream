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

func (q queryServer) ListMemberSeasonSnapshot(ctx context.Context, req *types.QueryAllMemberSeasonSnapshotRequest) (*types.QueryAllMemberSeasonSnapshotResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	memberSeasonSnapshots, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.MemberSeasonSnapshot,
		req.Pagination,
		func(_ string, value types.MemberSeasonSnapshot) (types.MemberSeasonSnapshot, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllMemberSeasonSnapshotResponse{MemberSeasonSnapshot: memberSeasonSnapshots, Pagination: pageRes}, nil
}

func (q queryServer) GetMemberSeasonSnapshot(ctx context.Context, req *types.QueryGetMemberSeasonSnapshotRequest) (*types.QueryGetMemberSeasonSnapshotResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.MemberSeasonSnapshot.Get(ctx, req.SeasonAddress)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetMemberSeasonSnapshotResponse{MemberSeasonSnapshot: val}, nil
}
