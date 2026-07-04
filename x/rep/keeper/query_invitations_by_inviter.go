package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) InvitationsByInviter(ctx context.Context, req *types.QueryInvitationsByInviterRequest) (*types.QueryInvitationsByInviterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	invitations, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Invitation,
		req.Pagination,
		func(_ uint64, value types.Invitation) (bool, error) {
			return value.Inviter == req.Inviter, nil
		},
		func(_ uint64, value types.Invitation) (types.Invitation, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryInvitationsByInviterResponse{Invitation: invitations, Pagination: pageRes}, nil
}
