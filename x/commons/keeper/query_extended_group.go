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

func (q queryServer) ListExtendedGroup(ctx context.Context, req *types.QueryAllExtendedGroupRequest) (*types.QueryAllExtendedGroupResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	extendedGroups, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.ExtendedGroup,
		req.Pagination,
		func(_ string, value types.ExtendedGroup) (types.ExtendedGroup, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllExtendedGroupResponse{ExtendedGroup: extendedGroups, Pagination: pageRes}, nil
}

func (q queryServer) GetExtendedGroup(ctx context.Context, req *types.QueryGetExtendedGroupRequest) (*types.QueryGetExtendedGroupResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.ExtendedGroup.Get(ctx, req.Index)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetExtendedGroupResponse{ExtendedGroup: val}, nil
}
