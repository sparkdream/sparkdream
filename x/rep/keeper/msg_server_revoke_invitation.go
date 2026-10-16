package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// RevokeInvitation lets the inviter withdraw a PENDING invitation (wrong
// address, changed mind) without waiting for the acceptance window to lapse.
// Closing it unlocks the stake, restores the credit and frees the invitee
// slot, exactly like expiry; the record stays with status REVOKED.
func (k msgServer) RevokeInvitation(ctx context.Context, msg *types.MsgRevokeInvitation) (*types.MsgRevokeInvitationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Inviter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid inviter address")
	}

	inv, err := k.Invitation.Get(ctx, msg.InvitationId)
	if err != nil {
		return nil, types.ErrInvitationNotFound
	}
	if inv.Inviter != msg.Inviter {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only the inviter can revoke an invitation")
	}
	if inv.Status != types.InvitationStatus_INVITATION_STATUS_PENDING {
		return nil, types.ErrInvitationNotPending
	}

	if err := k.closePendingInvitation(ctx, inv, types.InvitationStatus_INVITATION_STATUS_REVOKED, "invitation_revoked"); err != nil {
		return nil, err
	}
	return &types.MsgRevokeInvitationResponse{}, nil
}
