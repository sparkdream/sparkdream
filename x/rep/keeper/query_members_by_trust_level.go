package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MembersByTrustLevel(ctx context.Context, req *types.QueryMembersByTrustLevelRequest) (*types.QueryMembersByTrustLevelResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	members, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Member,
		req.Pagination,
		func(_ string, value types.Member) (bool, error) {
			return uint64(value.TrustLevel) == req.TrustLevel, nil
		},
		func(_ string, value types.Member) (types.Member, error) {
			_ = q.k.ApplyPendingDecay(ctx, &value)
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryMembersByTrustLevelResponse{Members: members, Pagination: pageRes}, nil
}
