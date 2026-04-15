package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GetBridgeOperator(ctx context.Context, req *types.QueryGetBridgeOperatorRequest) (*types.QueryGetBridgeOperatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	bridge, err := q.k.BridgeOperators.Get(ctx, collections.Join(req.Address, req.PeerId))
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not found for peer %s", req.Address, req.PeerId)
	}

	return &types.QueryGetBridgeOperatorResponse{BridgeOperator: bridge}, nil
}
