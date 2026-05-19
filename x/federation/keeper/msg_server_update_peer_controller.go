package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/federation/types"
)

// UpdatePeerController is a gov-authority message that changes a peer's
// controller_group. Affects only new bridge registrations under that
// peer; existing bindings keep the controller captured on their
// service.Operator at registration time. Transferring existing bridges
// requires service.MsgOpenControllerTransferCase.
//
// Phase 1 of the federation→service migration. Validation requires
// IsGroupPolicyAddress check against the proposed controller — wired
// through k.commonsKeeper.
func (k msgServer) UpdatePeerController(ctx context.Context, msg *types.MsgUpdatePeerController) (*types.MsgUpdatePeerControllerResponse, error) {
	if !k.IsGovAuthority(msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be x/gov authority")
	}

	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}

	if msg.ControllerGroup != "" {
		if k.late.commonsKeeper == nil {
			return nil, errorsmod.Wrap(types.ErrNotAuthorized, "commons keeper not wired")
		}
		if !k.late.commonsKeeper.IsGroupPolicyAddress(ctx, msg.ControllerGroup) {
			return nil, errorsmod.Wrapf(types.ErrNotAuthorized, "%s is not a registered group policy address", msg.ControllerGroup)
		}
	}

	peer.ControllerGroup = msg.ControllerGroup
	if err := k.Peers.Set(ctx, msg.PeerId, peer); err != nil {
		return nil, err
	}

	return &types.MsgUpdatePeerControllerResponse{}, nil
}
