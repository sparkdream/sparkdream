package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GetPendingIdentityChallenge(ctx context.Context, req *types.QueryGetPendingIdentityChallengeRequest) (*types.QueryGetPendingIdentityChallengeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	challenge, err := q.k.PendingIdChallenges.Get(ctx, collections.Join(req.ClaimedAddress, req.PeerId))
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrNoPendingChallenge, "no challenge for %s on peer %s", req.ClaimedAddress, req.PeerId)
	}

	return &types.QueryGetPendingIdentityChallengeResponse{Challenge: challenge}, nil
}
