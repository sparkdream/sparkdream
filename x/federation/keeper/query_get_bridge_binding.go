package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GetBridgeBinding(ctx context.Context, req *types.QueryGetBridgeBindingRequest) (*types.QueryGetBridgeBindingResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	binding, err := q.k.BridgeBindings.Get(ctx, collections.Join(req.Address, req.PeerId))
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "binding %s not found for peer %s", req.Address, req.PeerId)
	}

	return &types.QueryGetBridgeBindingResponse{BridgeBinding: binding}, nil
}
