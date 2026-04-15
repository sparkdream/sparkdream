package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListBridgeOperators(ctx context.Context, req *types.QueryListBridgeOperatorsRequest) (*types.QueryListBridgeOperatorsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var bridges []types.BridgeOperator
	err := q.k.BridgeOperators.Walk(ctx, nil, func(key collections.Pair[string, string], value types.BridgeOperator) (bool, error) {
		bridges = append(bridges, value)
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListBridgeOperatorsResponse{
		BridgeOperators: bridges,
	}, nil
}
