package keeper

import (
	"context"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListMember(ctx context.Context, req *types.QueryAllMemberRequest) (*types.QueryAllMemberResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	members, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Member,
		req.Pagination,
		func(_ string, value types.Member) (types.Member, error) {
			_ = q.k.ApplyPendingDecay(ctx, &value)
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllMemberResponse{Member: members, Pagination: pageRes}, nil
}

func (q queryServer) GetMember(ctx context.Context, req *types.QueryGetMemberRequest) (*types.QueryGetMemberResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.Member.Get(ctx, req.Address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	_ = q.k.ApplyPendingDecay(ctx, &val)

	return &types.QueryGetMemberResponse{Member: val}, nil
}
