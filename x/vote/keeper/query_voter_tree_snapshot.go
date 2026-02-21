package keeper

import (
	"context"
	"errors"

	"sparkdream/x/vote/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListVoterTreeSnapshot(ctx context.Context, req *types.QueryAllVoterTreeSnapshotRequest) (*types.QueryAllVoterTreeSnapshotResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	voterTreeSnapshots, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.VoterTreeSnapshot,
		req.Pagination,
		func(_ uint64, value types.VoterTreeSnapshot) (types.VoterTreeSnapshot, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllVoterTreeSnapshotResponse{VoterTreeSnapshot: voterTreeSnapshots, Pagination: pageRes}, nil
}

func (q queryServer) GetVoterTreeSnapshot(ctx context.Context, req *types.QueryGetVoterTreeSnapshotRequest) (*types.QueryGetVoterTreeSnapshotResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.VoterTreeSnapshot.Get(ctx, req.ProposalId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetVoterTreeSnapshotResponse{VoterTreeSnapshot: val}, nil
}
