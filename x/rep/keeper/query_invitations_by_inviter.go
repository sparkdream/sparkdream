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

	// Filtered walk over the whole collection, so matching every address the
	// inviter has held keeps pagination consistent.
	inviterAddrs := q.k.AllAddressesOf(ctx, req.Inviter)
	invitations, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Invitation,
		req.Pagination,
		func(_ uint64, value types.Invitation) (bool, error) {
			return AddressesMatch(inviterAddrs, value.Inviter), nil
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
