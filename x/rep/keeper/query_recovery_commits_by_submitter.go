package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RecoveryCommitsBySubmitter pages over the unexpired sealed recovery commits
// an address submitted. Seals expire after recovery_commit_ttl and each locks
// a deposit, so the set stays small enough to filter.
func (q queryServer) RecoveryCommitsBySubmitter(ctx context.Context, req *types.QueryRecoveryCommitsBySubmitterRequest) (*types.QueryRecoveryCommitsBySubmitterResponse, error) {
	if req == nil || req.Submitter == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	commits, pageRes, err := query.CollectionFilteredPaginate(
		ctx, q.k.RecoveryCommits, req.Pagination,
		func(_ []byte, c types.RecoveryCommit) (bool, error) {
			return c.Submitter == req.Submitter && c.ExpiresAt > now, nil
		},
		func(_ []byte, c types.RecoveryCommit) (types.RecoveryCommit, error) { return c, nil },
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryRecoveryCommitsBySubmitterResponse{Commits: commits, Pagination: pageRes}, nil
}
