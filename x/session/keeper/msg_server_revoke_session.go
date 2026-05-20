package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// RevokeSession is the legacy session-key revoke entrypoint. Internally
// finds the active SESSION_KEY grant for (granter, grantee) and removes
// it. The wire-level message shape and external behavior are preserved.
func (k msgServer) RevokeSession(ctx context.Context, msg *types.MsgRevokeSession) (*types.MsgRevokeSessionResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Authorization: the proto signer annotation is "granter", so the SDK ensures
	// msg.Granter is the tx signer. We verify the session exists for this exact
	// (granter, grantee) pair, so only the actual granter of a real session can revoke.
	id, err := k.SessionKeyByPair.Get(ctx, collections.Join(msg.Granter, msg.Grantee))
	if err != nil {
		return nil, types.ErrSessionNotFound
	}
	grant, err := k.Grants.Get(ctx, id)
	if err != nil {
		return nil, types.ErrSessionNotFound
	}
	sk := grant.GetSessionKey()
	if sk == nil {
		return nil, types.ErrSessionNotFound
	}

	if err := k.deleteGrant(ctx, grant); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"session_revoked",
		sdk.NewAttribute("granter", msg.Granter),
		sdk.NewAttribute("grantee", msg.Grantee),
		sdk.NewAttribute("exec_count", fmt.Sprintf("%d", sk.ExecCount)),
		sdk.NewAttribute("spent", sk.Spent.String()),
		sdk.NewAttribute("grant_id", fmt.Sprintf("%d", id)),
	))

	return &types.MsgRevokeSessionResponse{}, nil
}
