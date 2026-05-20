package keeper

import (
	"context"
	"errors"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/commons/types"
	sessiontypes "sparkdream/x/session/types"
)

// CancelRecurringSpend terminates an active schedule. The signer must
// be the same authority that created it — when wrapped in a
// MsgSubmitProposal this means the same council must vote to cancel.
//
// **M6 (RecurringSpend migration):** the handler is a thin wrapper
// that looks up the grant in session.Grants, verifies the caller's
// authority matches the grant's granter, and calls
// `sessionKeeper.RevokeGrantInternal`. The grant is then DELETED
// (session does not keep tombstones on revoke); the audit trail
// lives in the `grant_revoked` event emitted by session.
//
// Error contract: session returns `ErrGrantNotFound` for an already-
// cancelled / non-existent grant and `ErrGrantTerminal` for COMPLETED
// grants. Both are translated to `ErrRecurringSpendInactive` so the
// wrapper preserves today's public error contract for double-cancel
// callers (see migration plan §8 / L7).
func (k msgServer) CancelRecurringSpend(goCtx context.Context, msg *types.MsgCancelRecurringSpend) (*types.MsgCancelRecurringSpendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid authority address")
	}

	if k.late.sessionKeeper == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic,
			"session keeper not wired (set via app.CommonsKeeper.SetSessionKeeper)")
	}

	// Look up the grant to enforce the "only the original authority can
	// cancel" rule. Session's RevokeGrantInternal does NOT check this
	// (the bypass surface trusts the caller module to authorize).
	grant, err := k.late.sessionKeeper.GetGrant(ctx, msg.Id)
	if err != nil {
		if errors.Is(err, sessiontypes.ErrGrantNotFound) {
			return nil, errorsmod.Wrapf(types.ErrRecurringSpendInactive,
				"schedule %d not found (already terminal or never existed)", msg.Id)
		}
		return nil, err
	}
	if grant.Granter != msg.Authority {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendUnauthorized,
			"caller %s is not the schedule's authority %s", msg.Authority, grant.Granter)
	}

	commonsModuleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	if _, err := k.late.sessionKeeper.RevokeGrantInternal(ctx, commonsModuleAddr, msg.Id); err != nil {
		// COMPLETED grants return ErrGrantTerminal — translate so the
		// wrapper preserves the legacy public error.
		if errors.Is(err, sessiontypes.ErrGrantTerminal) {
			return nil, errorsmod.Wrapf(types.ErrRecurringSpendInactive,
				"schedule %d is in a terminal status", msg.Id)
		}
		return nil, err
	}

	// No legacy commons-side state to touch (no parallel storage,
	// no decActiveCount — session handles its own active-grant
	// counter). No legacy `recurring_spend_canceled` event —
	// session emits `grant_revoked` with `source=module_bypass`.

	return &types.MsgCancelRecurringSpendResponse{}, nil
}
