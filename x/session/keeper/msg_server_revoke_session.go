package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

func (k msgServer) RevokeSession(ctx context.Context, msg *types.MsgRevokeSession) (*types.MsgRevokeSessionResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Authorization: the proto signer annotation is "granter", so the SDK ensures
	// msg.Granter is the tx signer. We verify the session exists for this exact
	// (granter, grantee) pair, so only the actual granter of a real session can revoke.

	key := collections.Join(msg.Granter, msg.Grantee)
	session, err := k.Sessions.Get(ctx, key)
	if err != nil {
		return nil, types.ErrSessionNotFound
	}

	// Delete session and all indexes
	if err := k.Sessions.Remove(ctx, key); err != nil {
		return nil, err
	}
	if err := k.SessionsByGranter.Remove(ctx, collections.Join(msg.Granter, msg.Grantee)); err != nil {
		return nil, err
	}
	if err := k.SessionsByGrantee.Remove(ctx, collections.Join(msg.Grantee, msg.Granter)); err != nil {
		return nil, err
	}
	if err := k.SessionsByExpiration.Remove(ctx, collections.Join3(session.Expiration.Unix(), msg.Granter, msg.Grantee)); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"session_revoked",
		sdk.NewAttribute("granter", msg.Granter),
		sdk.NewAttribute("grantee", msg.Grantee),
		sdk.NewAttribute("exec_count", fmt.Sprintf("%d", session.ExecCount)),
		sdk.NewAttribute("spent", session.Spent.String()),
	))

	return &types.MsgRevokeSessionResponse{}, nil
}
