package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
)

// RevokeIdentityLink removes an identity link by an executed committee
// decision (FED-105). A link a bridge operator attested is otherwise
// returned to UNVERIFIED only when that operator is slashed out of
// x/service (Phase 3b); once the operator RETIRES, a false attestation
// would stand forever and the real owner of the remote identity could
// never link it. The link goes with its indexes and its share of the
// owner's link count, exactly as a peer-removal cleanup removes it.
func (k msgServer) RevokeIdentityLink(ctx context.Context, msg *types.MsgRevokeIdentityLink) (*types.MsgRevokeIdentityLinkResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}
	if !k.isCouncilPolicy(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or the Commons Council / Operations Committee policy address")
	}
	if len(msg.Reason) > types.MaxInboundIdentifierSize {
		return nil, errorsmod.Wrapf(types.ErrInvalidRequest, "reason is %d bytes, max %d", len(msg.Reason), types.MaxInboundIdentifierSize)
	}

	link, err := k.IdentityLinks.Get(ctx, collections.Join(msg.LocalAddress, msg.PeerId))
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIdentityLinkNotFound, "no link for %s on peer %s", msg.LocalAddress, msg.PeerId)
	}

	k.revokeIdentityLink(ctx, link, "revoked_by_authority: "+msg.Reason)
	return &types.MsgRevokeIdentityLinkResponse{}, nil
}
