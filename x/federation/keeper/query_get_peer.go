package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GetPeer(ctx context.Context, req *types.QueryGetPeerRequest) (*types.QueryGetPeerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	peer, err := q.k.Peers.Get(ctx, req.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", req.PeerId)
	}

	return &types.QueryGetPeerResponse{Peer: peer}, nil
}
