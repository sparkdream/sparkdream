package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GetPeerPolicy(ctx context.Context, req *types.QueryGetPeerPolicyRequest) (*types.QueryGetPeerPolicyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	policy, err := q.k.PeerPolicies.Get(ctx, req.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "policy for peer %q not found", req.PeerId)
	}

	return &types.QueryGetPeerPolicyResponse{Policy: policy}, nil
}
