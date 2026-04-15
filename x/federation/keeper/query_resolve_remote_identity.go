package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ResolveRemoteIdentity(ctx context.Context, req *types.QueryResolveRemoteIdentityRequest) (*types.QueryResolveRemoteIdentityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	localAddr, err := q.k.IdentityLinksByRemote.Get(ctx, collections.Join(req.PeerId, req.RemoteIdentity))
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIdentityLinkNotFound, "no link for remote identity %s on peer %s", req.RemoteIdentity, req.PeerId)
	}

	return &types.QueryResolveRemoteIdentityResponse{LocalAddress: localAddr}, nil
}
