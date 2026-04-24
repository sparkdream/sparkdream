package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/types"
)

// BondedRole returns a single BondedRole record for (role_type, address).
func (q queryServer) BondedRole(ctx context.Context, req *types.QueryBondedRoleRequest) (*types.QueryBondedRoleResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.RoleType == types.RoleType_ROLE_TYPE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "role_type required")
	}
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address required")
	}
	val, err := q.k.BondedRoles.Get(ctx, collections.Join(int32(req.RoleType), req.Address))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "bonded role not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryBondedRoleResponse{BondedRole: val}, nil
}

// BondedRolesByType lists all BondedRole records for the given role_type,
// with pagination over the (role_type, address) prefix.
func (q queryServer) BondedRolesByType(ctx context.Context, req *types.QueryBondedRolesByTypeRequest) (*types.QueryBondedRolesByTypeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.RoleType == types.RoleType_ROLE_TYPE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "role_type required")
	}
	items, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.BondedRoles,
		req.Pagination,
		func(_ collections.Pair[int32, string], value types.BondedRole) (types.BondedRole, error) {
			return value, nil
		},
		query.WithCollectionPaginationPairPrefix[int32, string](int32(req.RoleType)),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryBondedRolesByTypeResponse{BondedRoles: items, Pagination: pageRes}, nil
}

// BondedRoleConfig returns the policy config for a role_type.
func (q queryServer) BondedRoleConfig(ctx context.Context, req *types.QueryBondedRoleConfigRequest) (*types.QueryBondedRoleConfigResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.RoleType == types.RoleType_ROLE_TYPE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "role_type required")
	}
	cfg, err := q.k.BondedRoleConfigs.Get(ctx, int32(req.RoleType))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "bonded role config not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryBondedRoleConfigResponse{BondedRoleConfig: cfg}, nil
}
