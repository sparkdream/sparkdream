package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/session/types"
)

// AcceptGrant is the grantee's explicit consent to a PENDING access-class
// grant (SES-102). It is the only acceptance path for a SIGN_IN grant
// (nothing executes through one, so an off-chain service like sdaplogin
// never sees it until it is accepted) and an alternative to the implicit
// first MsgExecSession for a session key. Never session-delegable: the
// consent must be the grantee key's own signature.
func (k msgServer) AcceptGrant(ctx context.Context, msg *types.MsgAcceptGrant) (*types.MsgAcceptGrantResponse, error) {
	grant, err := k.Grants.Get(ctx, msg.GrantId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(types.ErrGrantNotFound, "id=%d", msg.GrantId)
		}
		return nil, err
	}
	if msg.Grantee != grant.Grantee {
		return nil, errorsmod.Wrapf(types.ErrAcceptUnauthorized,
			"caller %s is not the grantee %s", msg.Grantee, grant.Grantee)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	_, refund, err := k.acceptPendingGrant(ctx, params, grant)
	if err != nil {
		return nil, err
	}
	return &types.MsgAcceptGrantResponse{RefundAmount: refund}, nil
}
