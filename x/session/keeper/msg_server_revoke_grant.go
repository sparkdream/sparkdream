package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// RevokeGrant is the unified revoke entrypoint. The caller authorizes
// the revocation in one of two ways:
//
//  1. msg.Granter exactly matches grant.Granter — the granter directly
//     cancels their own grant (most common path).
//  2. msg.Granter is the grantee of a SESSION_KEY grant whose payload
//     has allow_self_revoke = true AND the targeted grant's granter
//     matches that session key's granter. This is the
//     "let-my-UI-key-tidy-up-stale-grants" cleanup workflow (Rev 2
//     §7.2). Cross-granter revocation via a session key remains
//     impossible.
//
// On success the grant is deleted; for ScheduledOneshot grants the
// OneshotGasDeposit is refunded to the granter (Rev 2 §4.4.4(d)).
// The legacy MsgRevokeSession handler continues to live alongside this
// for the (granter, grantee) lookup path.
func (k msgServer) RevokeGrant(ctx context.Context, msg *types.MsgRevokeGrant) (*types.MsgRevokeGrantResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	grant, err := k.Grants.Get(ctx, msg.GrantId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(types.ErrGrantNotFound, "id=%d", msg.GrantId)
		}
		return nil, err
	}

	// Terminal status check — terminal grants are already gone from
	// the active counter and indexes; rejecting here is the right UX.
	if terminalStatus(grant.Status) {
		return nil, errorsmod.Wrapf(types.ErrGrantTerminal,
			"grant %d is %s (terminal)", grant.Id, grant.Status)
	}

	// Path 1: direct granter revoke.
	if msg.Granter != grant.Granter {
		// Path 2: session-key allow_self_revoke. msg.Granter must be the
		// grantee of an ACTIVE SESSION_KEY grant whose granter matches
		// grant.Granter AND whose payload has allow_self_revoke = true.
		skID, lookupErr := k.SessionKeyByPair.Get(ctx, collections.Join(grant.Granter, msg.Granter))
		if lookupErr != nil {
			return nil, errorsmod.Wrapf(types.ErrRevokeUnauthorized,
				"caller %s is neither the granter nor a same-granter session key",
				msg.Granter)
		}
		skGrant, err := k.Grants.Get(ctx, skID)
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrRevokeUnauthorized, "session-key grant lookup failed")
		}
		sk := skGrant.GetSessionKey()
		if sk == nil || skGrant.Status != types.GrantStatus_GRANT_STATUS_ACTIVE {
			return nil, errorsmod.Wrap(types.ErrRevokeUnauthorized, "no active session-key grant for caller")
		}
		if !sk.AllowSelfRevoke {
			return nil, types.ErrSelfRevokeNotPermitted
		}
		// Defense-in-depth: confirm the targeted grant's granter
		// matches the session-key's granter. (Already guaranteed by
		// the SessionKeyByPair lookup above; restated for clarity.)
		if skGrant.Granter != grant.Granter {
			return nil, errorsmod.Wrap(types.ErrCrossGranterRevoke, "session-key revoke target granter mismatch")
		}
	}

	// Refund any held oneshot deposit BEFORE deleting the grant.
	refund := sdk.NewCoin("uspark", sdkmath.ZeroInt())
	if grant.Type == types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT {
		dep, depErr := k.OneshotGasDeposit.Get(ctx, grant.Id)
		if depErr == nil && !dep.IsZero() {
			granterAddr, addrErr := k.addressCodec.StringToBytes(grant.Granter)
			if addrErr != nil {
				return nil, addrErr
			}
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, granterAddr, sdk.NewCoins(dep)); err != nil {
				return nil, errorsmod.Wrap(err, "deposit refund failed")
			}
			_ = k.OneshotGasDeposit.Remove(ctx, grant.Id)
			refund = dep
		}
	}

	// deleteGrant drops the active-counter, every secondary index, and
	// the primary record.
	if err := k.deleteGrant(ctx, grant); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"grant_revoked",
		sdk.NewAttribute("id", fmt.Sprintf("%d", grant.Id)),
		sdk.NewAttribute("type", grant.Type.String()),
		sdk.NewAttribute("granter", grant.Granter),
		sdk.NewAttribute("grantee", grant.Grantee),
		sdk.NewAttribute("refund_amount", refund.String()),
	))

	return &types.MsgRevokeGrantResponse{RefundAmount: refund}, nil
}
