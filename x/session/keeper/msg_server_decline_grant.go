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

// DeclineGrant lets the grantee opt out of any active grant. Applies
// uniformly to all four grant types (Rev 2 §2.3, §M2-M3).
//
// State transitions:
//   - SESSION_KEY: grant -> DECLINED, removed from active counter and
//     all secondary indexes. The (granter, grantee) pair is freed so
//     the granter could create a fresh session key for the same
//     grantee. The session key cannot be used for any further
//     MsgExecSession after decline.
//   - RECURRING_PULL / SPENDING_ALLOWANCE: same shape — grant -> DECLINED,
//     no further claims accepted from the grantee.
//   - SCHEDULED_ONESHOT: decline before fire is a veto. The grant
//     transitions to DECLINED, the EndBlocker fire pass skips it
//     (status filter), and the held OneshotGasDeposit is refunded to
//     the granter (Rev 3 §M3).
//
// Decline is one-way. The granter must Revoke + CreateGrant to issue a
// fresh grant.
//
// We delete the grant entirely on decline (same as Revoke) rather than
// leaving a DECLINED tombstone. Pre-launch convention: terminal states
// drop their primary-store entry; state is observable via the
// `grant_declined` event for indexers.
func (k msgServer) DeclineGrant(ctx context.Context, msg *types.MsgDeclineGrant) (*types.MsgDeclineGrantResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	grant, err := k.Grants.Get(ctx, msg.GrantId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(types.ErrGrantNotFound, "id=%d", msg.GrantId)
		}
		return nil, err
	}

	if msg.Grantee != grant.Grantee {
		return nil, errorsmod.Wrapf(types.ErrDeclineUnauthorized,
			"caller %s is not the grantee %s", msg.Grantee, grant.Grantee)
	}

	if terminalStatus(grant.Status) {
		return nil, errorsmod.Wrapf(types.ErrGrantTerminal,
			"grant %d is %s (terminal; decline is one-way)", grant.Id, grant.Status)
	}

	// Refund any held oneshot deposit (Rev 3 §M3 — easy to miss
	// otherwise; mirrored from the revoke path).
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

	if err := k.deleteGrant(ctx, grant); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"grant_declined",
		sdk.NewAttribute("id", fmt.Sprintf("%d", grant.Id)),
		sdk.NewAttribute("type", grant.Type.String()),
		sdk.NewAttribute("granter", grant.Granter),
		sdk.NewAttribute("grantee", grant.Grantee),
		sdk.NewAttribute("refund_amount", refund.String()),
	))

	return &types.MsgDeclineGrantResponse{RefundAmount: refund}, nil
}
