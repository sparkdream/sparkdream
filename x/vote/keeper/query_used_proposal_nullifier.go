package keeper

import (
	"context"
	"errors"

	"sparkdream/x/vote/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListUsedProposalNullifier(ctx context.Context, req *types.QueryAllUsedProposalNullifierRequest) (*types.QueryAllUsedProposalNullifierResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	usedProposalNullifiers, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.UsedProposalNullifier,
		req.Pagination,
		func(_ string, value types.UsedProposalNullifier) (types.UsedProposalNullifier, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllUsedProposalNullifierResponse{UsedProposalNullifier: usedProposalNullifiers, Pagination: pageRes}, nil
}

func (q queryServer) GetUsedProposalNullifier(ctx context.Context, req *types.QueryGetUsedProposalNullifierRequest) (*types.QueryGetUsedProposalNullifierResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.UsedProposalNullifier.Get(ctx, req.Index)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetUsedProposalNullifierResponse{UsedProposalNullifier: val}, nil
}
