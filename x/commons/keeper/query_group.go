package keeper

import (
	"context"
	"errors"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListGroups(ctx context.Context, req *types.QueryAllGroupRequest) (*types.QueryAllGroupResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	groups, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Groups,
		req.Pagination,
		func(key string, value types.Group) (types.Group, error) {
			value.Index = key
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllGroupResponse{Group: groups, Pagination: pageRes}, nil
}

func (q queryServer) GetGroup(ctx context.Context, req *types.QueryGetGroupRequest) (*types.QueryGetGroupResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.Groups.Get(ctx, req.Index)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	val.Index = req.Index
	return &types.QueryGetGroupResponse{Group: val}, nil
}
