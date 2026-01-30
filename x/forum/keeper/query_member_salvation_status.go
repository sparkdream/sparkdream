package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListMemberSalvationStatus(ctx context.Context, req *types.QueryAllMemberSalvationStatusRequest) (*types.QueryAllMemberSalvationStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	memberSalvationStatuss, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.MemberSalvationStatus,
		req.Pagination,
		func(_ string, value types.MemberSalvationStatus) (types.MemberSalvationStatus, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllMemberSalvationStatusResponse{MemberSalvationStatus: memberSalvationStatuss, Pagination: pageRes}, nil
}

func (q queryServer) GetMemberSalvationStatus(ctx context.Context, req *types.QueryGetMemberSalvationStatusRequest) (*types.QueryGetMemberSalvationStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.MemberSalvationStatus.Get(ctx, req.Address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetMemberSalvationStatusResponse{MemberSalvationStatus: val}, nil
}
