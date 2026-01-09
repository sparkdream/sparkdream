package keeper

import (
	"context"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListInvitation(ctx context.Context, req *types.QueryAllInvitationRequest) (*types.QueryAllInvitationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	invitations, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Invitation,
		req.Pagination,
		func(_ uint64, value types.Invitation) (types.Invitation, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllInvitationResponse{Invitation: invitations, Pagination: pageRes}, nil
}

func (q queryServer) GetInvitation(ctx context.Context, req *types.QueryGetInvitationRequest) (*types.QueryGetInvitationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	invitation, err := q.k.Invitation.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetInvitationResponse{Invitation: invitation}, nil
}
