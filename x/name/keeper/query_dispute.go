package keeper

import (
	"context"
	"errors"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListDispute(ctx context.Context, req *types.QueryAllDisputeRequest) (*types.QueryAllDisputeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	disputes, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Disputes,
		req.Pagination,
		func(_ string, value types.Dispute) (types.Dispute, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllDisputeResponse{Dispute: disputes, Pagination: pageRes}, nil
}

func (q queryServer) GetDispute(ctx context.Context, req *types.QueryGetDisputeRequest) (*types.QueryGetDisputeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.Disputes.Get(ctx, req.Name)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetDisputeResponse{Dispute: val}, nil
}
