package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetEscalatedChallenge returns the Phase 2 (jury) lifecycle record for
// a content_id. Returns ErrEscalatedChallengeNotFound when no jury
// lifecycle is currently open (no escalation has fired or the verdict
// has already been applied).
func (q queryServer) GetEscalatedChallenge(ctx context.Context, req *types.QueryGetEscalatedChallengeRequest) (*types.QueryGetEscalatedChallengeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	esc, err := q.k.EscalatedChallenges.Get(ctx, req.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrEscalatedChallengeNotFound, "content_id=%d", req.ContentId)
	}
	return &types.QueryGetEscalatedChallengeResponse{Escalated: esc}, nil
}
