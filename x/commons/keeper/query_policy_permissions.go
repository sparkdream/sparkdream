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

func (q queryServer) ListPolicyPermissions(ctx context.Context, req *types.QueryAllPolicyPermissionsRequest) (*types.QueryAllPolicyPermissionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	policyPermissionss, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.PolicyPermissions,
		req.Pagination,
		func(_ string, value types.PolicyPermissions) (types.PolicyPermissions, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllPolicyPermissionsResponse{PolicyPermissions: policyPermissionss, Pagination: pageRes}, nil
}

func (q queryServer) GetPolicyPermissions(ctx context.Context, req *types.QueryGetPolicyPermissionsRequest) (*types.QueryGetPolicyPermissionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.PolicyPermissions.Get(ctx, req.PolicyAddress)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetPolicyPermissionsResponse{PolicyPermissions: val}, nil
}
