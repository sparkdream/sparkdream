package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) InvitationsByInviter(ctx context.Context, req *types.QueryInvitationsByInviterRequest) (*types.QueryInvitationsByInviterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect first invitation from the specified inviter (proto response is singular)
	var foundInvitation *types.Invitation
	err := q.k.Invitation.Walk(ctx, nil, func(id uint64, invitation types.Invitation) (bool, error) {
		if invitation.Inviter == req.Inviter {
			foundInvitation = &invitation
			return true, nil // stop iteration
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundInvitation != nil {
		return &types.QueryInvitationsByInviterResponse{
			InvitationId:   foundInvitation.Id,
			InviteeAddress: foundInvitation.InviteeAddress,
			Status:         uint64(foundInvitation.Status),
		}, nil
	}

	return &types.QueryInvitationsByInviterResponse{}, nil
}
