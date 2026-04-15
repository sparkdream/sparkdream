package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GetIdentityLink(ctx context.Context, req *types.QueryGetIdentityLinkRequest) (*types.QueryGetIdentityLinkResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	link, err := q.k.IdentityLinks.Get(ctx, collections.Join(req.LocalAddress, req.PeerId))
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIdentityLinkNotFound, "no link for %s on peer %s", req.LocalAddress, req.PeerId)
	}

	return &types.QueryGetIdentityLinkResponse{Link: link}, nil
}
