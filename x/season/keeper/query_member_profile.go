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

func (q queryServer) ListMemberProfile(ctx context.Context, req *types.QueryAllMemberProfileRequest) (*types.QueryAllMemberProfileResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	memberProfiles, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.MemberProfile,
		req.Pagination,
		func(_ string, value types.MemberProfile) (types.MemberProfile, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllMemberProfileResponse{MemberProfile: memberProfiles, Pagination: pageRes}, nil
}

func (q queryServer) GetMemberProfile(ctx context.Context, req *types.QueryGetMemberProfileRequest) (*types.QueryGetMemberProfileResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.MemberProfile.Get(ctx, req.Address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetMemberProfileResponse{MemberProfile: val}, nil
}
