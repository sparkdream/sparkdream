package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
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

	// A PENDING key holds the granter's consent deposit (SES-102): return
	// it before the grant goes, exactly as MsgRevokeGrant does, or the escrow
	// row would outlive the grant (SES-201).
	refund := sdk.NewCoin(k.BondDenom(ctx), sdkmath.ZeroInt())
	if grant.Status == types.GrantStatus_GRANT_STATUS_PENDING {
		if refund, err = k.refundPendingDeposit(ctx, grant, grant.Granter); err != nil {
			return nil, err
		}
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
		sdk.NewAttribute("refund_amount", refund.String()),
	))

	return &types.MsgRevokeSessionResponse{}, nil
}
