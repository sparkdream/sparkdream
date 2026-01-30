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

func (q queryServer) ListEpochXpTracker(ctx context.Context, req *types.QueryAllEpochXpTrackerRequest) (*types.QueryAllEpochXpTrackerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	epochXpTrackers, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.EpochXpTracker,
		req.Pagination,
		func(_ string, value types.EpochXpTracker) (types.EpochXpTracker, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllEpochXpTrackerResponse{EpochXpTracker: epochXpTrackers, Pagination: pageRes}, nil
}

func (q queryServer) GetEpochXpTracker(ctx context.Context, req *types.QueryGetEpochXpTrackerRequest) (*types.QueryGetEpochXpTrackerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.EpochXpTracker.Get(ctx, req.MemberEpoch)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetEpochXpTrackerResponse{EpochXpTracker: val}, nil
}
