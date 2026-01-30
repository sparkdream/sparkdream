package keeper

import (
	"context"
	"errors"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListMemberRegistration(ctx context.Context, req *types.QueryAllMemberRegistrationRequest) (*types.QueryAllMemberRegistrationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	memberRegistrations, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.MemberRegistration,
		req.Pagination,
		func(_ string, value types.MemberRegistration) (types.MemberRegistration, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllMemberRegistrationResponse{MemberRegistration: memberRegistrations, Pagination: pageRes}, nil
}

func (q queryServer) GetMemberRegistration(ctx context.Context, req *types.QueryGetMemberRegistrationRequest) (*types.QueryGetMemberRegistrationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.MemberRegistration.Get(ctx, req.Member)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetMemberRegistrationResponse{MemberRegistration: val}, nil
}
