package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListMemberWarning(ctx context.Context, req *types.QueryAllMemberWarningRequest) (*types.QueryAllMemberWarningResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	memberWarnings, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.MemberWarning,
		req.Pagination,
		func(_ uint64, value types.MemberWarning) (types.MemberWarning, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllMemberWarningResponse{MemberWarning: memberWarnings, Pagination: pageRes}, nil
}

func (q queryServer) GetMemberWarning(ctx context.Context, req *types.QueryGetMemberWarningRequest) (*types.QueryGetMemberWarningResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	memberWarning, err := q.k.MemberWarning.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetMemberWarningResponse{MemberWarning: memberWarning}, nil
}
