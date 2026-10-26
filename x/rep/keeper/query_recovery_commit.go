package keeper

import (
	"context"
	"encoding/hex"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RecoveryCommit returns the unexpired sealed recovery commit with the given
// seal, or NotFound (also once it expired, even before the sweep removes it).
func (q queryServer) RecoveryCommit(ctx context.Context, req *types.QueryRecoveryCommitRequest) (*types.QueryRecoveryCommitResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	sealed, err := hex.DecodeString(req.SealedHex)
	if err != nil || len(sealed) != types.RecoveryCommitmentSize {
		return nil, status.Error(codes.InvalidArgument, "sealed_hex must be 32 bytes of hex")
	}
	commit, err := q.k.RecoveryCommits.Get(ctx, sealed)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "no recovery commit")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	if commit.ExpiresAt <= sdk.UnwrapSDKContext(ctx).BlockTime().Unix() {
		return nil, status.Error(codes.NotFound, "no recovery commit")
	}
	return &types.QueryRecoveryCommitResponse{Commit: commit}, nil
}
