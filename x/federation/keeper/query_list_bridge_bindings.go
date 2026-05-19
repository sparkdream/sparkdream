package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListBridgeBindings(ctx context.Context, req *types.QueryListBridgeBindingsRequest) (*types.QueryListBridgeBindingsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	bindings, pageRes, err := query.CollectionPaginate(
		ctx, q.k.BridgeBindings, req.Pagination,
		func(_ collections.Pair[string, string], value types.BridgeBinding) (types.BridgeBinding, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListBridgeBindingsResponse{
		BridgeBindings: bindings,
		Pagination:     pageRes,
	}, nil
}
