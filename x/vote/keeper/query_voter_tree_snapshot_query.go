package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) VoterTreeSnapshotQuery(ctx context.Context, req *types.QueryVoterTreeSnapshotQueryRequest) (*types.QueryVoterTreeSnapshotQueryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	snapshot, err := q.k.VoterTreeSnapshot.Get(ctx, req.ProposalId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "voter tree snapshot not found")
	}

	return &types.QueryVoterTreeSnapshotQueryResponse{Snapshot: snapshot}, nil
}
