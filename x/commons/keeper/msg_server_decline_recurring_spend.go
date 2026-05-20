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

// DeclineRecurringSpend lets the recipient permanently opt out of a
// schedule — e.g. when leaving a role. After decline, no further
// claims succeed and the council can re-schedule (with a new id) to
// designate someone else.
//
// **M7 (RecurringSpend migration):** the handler is a thin wrapper
// that calls `sessionKeeper.DeclineGrantInternal` after verifying the
// caller is the grantee on file. The grant is then DELETED (session
// does not keep tombstones on decline); the audit trail lives in the
// `grant_declined` event emitted by session.
//
// Error contract: `ErrGrantNotFound` and `ErrGrantTerminal` from
// session are translated to `ErrRecurringSpendInactive` for parity
// with the pre-migration wrapper.
func (k msgServer) DeclineRecurringSpend(goCtx context.Context, msg *types.MsgDeclineRecurringSpend) (*types.MsgDeclineRecurringSpendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if _, err := k.addressCodec.StringToBytes(msg.Recipient); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid recipient address")
	}

	if k.late.sessionKeeper == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic,
			"session keeper not wired (set via app.CommonsKeeper.SetSessionKeeper)")
	}

	// Verify the caller is the recipient on file. Session's
	// DeclineGrantInternal also checks this, but checking on the
	// commons side first lets us surface the same
	// ErrRecurringSpendUnauthorized error as before.
	grant, err := k.late.sessionKeeper.GetGrant(ctx, msg.Id)
	if err != nil {
		if errors.Is(err, sessiontypes.ErrGrantNotFound) {
			return nil, errorsmod.Wrapf(types.ErrRecurringSpendInactive,
				"schedule %d not found (already terminal or never existed)", msg.Id)
		}
		return nil, err
	}
	if grant.Grantee != msg.Recipient {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendUnauthorized,
			"caller %s is not the schedule's recipient %s", msg.Recipient, grant.Grantee)
	}

	commonsModuleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	if _, err := k.late.sessionKeeper.DeclineGrantInternal(ctx, commonsModuleAddr, msg.Id, msg.Recipient); err != nil {
		if errors.Is(err, sessiontypes.ErrGrantTerminal) {
			return nil, errorsmod.Wrapf(types.ErrRecurringSpendInactive,
				"schedule %d is in a terminal status", msg.Id)
		}
		return nil, err
	}

	// No legacy `recurring_spend_declined` event — session emits
	// `grant_declined` with `source=module_bypass`.

	return &types.MsgDeclineRecurringSpendResponse{}, nil
}
