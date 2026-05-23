package keeper

import (
	"context"
	"time"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/commons/types"
	sessiontypes "sparkdream/x/session/types"
)

// ScheduleRecurringSpend records a council-approved recurring
// disbursement. The signer (authority) must be a registered council
// policy address; callers are expected to wrap this in a
// MsgSubmitProposal so it executes only after a council vote.
//
// **M5 (RecurringSpend migration):** the handler is now a thin
// wrapper that constructs a session `MsgCreateGrant` carrying a
// `RecurringPullPayload` and calls
// `sessionKeeper.CreateGrantOnBehalfOf`. The schedule now lives in
// the unified session.Grants store; no row is written to the legacy
// commons.RecurringSpends collection. The single-coin restriction
// (D1.a) is enforced here; session does the rest of the validation
// (period bounds, denom allowlist, active-grant cap, etc.).
//
// `max_per_epoch` on the underlying grant is computed
// nil-safely from the council's own `MaxSpendPerEpoch`:
//   - if the council has no per-epoch cap set, default to
//     10 × amount_per_period (session's documented default),
//   - otherwise use `max(*MaxSpendPerEpoch, 10 × amount_per_period)`
//     so the per-grant cap never binds before the council-wide cap
//     (which is the actual policy gate via SessionClaimHook.PreCheck).
func (k msgServer) ScheduleRecurringSpend(goCtx context.Context, msg *types.MsgScheduleRecurringSpend) (*types.MsgScheduleRecurringSpendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Address sanity.
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid authority address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Recipient); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid recipient address")
	}

	// 2. Authority must be a registered group policy. Same lookup as
	// MsgSpendFromCommons so the two paths share gating semantics. We
	// keep this commons-side because session can't tell a council
	// policy apart from a regular user account.
	_, extGroup, found := k.GetGroupByPolicy(ctx, msg.Authority)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound,
			"signer %s is not a registered group policy", msg.Authority)
	}

	// 3. Amount sanity + D1.a single-coin restriction. Council schedules
	// are 1 coin per schedule; multi-coin payments require multiple
	// schedules. Documented behavior, not a regression for any current
	// caller (all existing schedules use a single uspark coin).
	if !msg.AmountPerPeriod.IsValid() || msg.AmountPerPeriod.IsZero() {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "amount_per_period must be valid non-zero coins")
	}
	if len(msg.AmountPerPeriod) != 1 {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"amount_per_period must contain exactly one coin (got %d); multi-coin schedules are not supported (D1.a)",
			len(msg.AmountPerPeriod))
	}
	amount := msg.AmountPerPeriod[0]

	// 4. Note bound. Session also enforces 256 chars; we check here
	// too so the error attribution is clearly commons-side.
	if len(msg.Note) > MaxRecurringSpendNoteLen {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"note exceeds %d chars (got %d)", MaxRecurringSpendNoteLen, len(msg.Note))
	}

	// 5. Window sanity. Session validates period >= min and
	// (expires_at - start_time) <= max_recurring_duration, but does
	// NOT enforce "window must contain at least one period". Keep
	// the latter commons-side so a council can't authorize a
	// never-claimable schedule.
	now := ctx.BlockTime().Unix()
	start := msg.StartTime
	if start == 0 {
		start = now
	}
	if start < now {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendInvalidWindow,
			"start_time %d is in the past (now=%d)", start, now)
	}
	if msg.EndTime <= start {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendInvalidWindow,
			"end_time %d must be > start_time %d", msg.EndTime, start)
	}
	if msg.EndTime-start < msg.PeriodSeconds {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendInvalidWindow,
			"window (%d s) shorter than one period (%d s) — no claim could ever succeed",
			msg.EndTime-start, msg.PeriodSeconds)
	}

	// 6. Compute max_per_epoch per the nil-safe formula
	// documented in §4 of the migration plan. The council-wide cap is
	// the real policy gate (enforced by SessionClaimHook.PreCheck);
	// the per-grant value just has to be ≥ that cap so it never binds
	// first.
	defaultPerGrant := amount.Amount.MulRaw(10) // 10× amount_per_period
	var maxPerEpoch math.Int
	if extGroup.MaxSpendPerEpoch == nil || extGroup.MaxSpendPerEpoch.IsZero() {
		maxPerEpoch = defaultPerGrant
	} else {
		councilCap := *extGroup.MaxSpendPerEpoch
		if councilCap.GT(defaultPerGrant) {
			maxPerEpoch = councilCap
		} else {
			maxPerEpoch = defaultPerGrant
		}
	}

	// 7. Construct the session MsgCreateGrant and call the bypass.
	sessionMsg := &sessiontypes.MsgCreateGrant{
		Granter:   msg.Authority,
		Grantee:   msg.Recipient,
		ExpiresAt: time.Unix(msg.EndTime, 0).UTC(),
		Note:      msg.Note,
		Payload: &sessiontypes.MsgCreateGrant_RecurringPull{
			RecurringPull: &sessiontypes.RecurringPullPayload{
				AmountPerPeriod: amount,
				PeriodSeconds:   msg.PeriodSeconds,
				StartTime:       start,
				MaxPerEpoch:     maxPerEpoch.String(),
			},
		},
	}

	if k.late.sessionKeeper == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic,
			"session keeper not wired (set via app.CommonsKeeper.SetSessionKeeper)")
	}
	commonsModuleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	grantID, err := k.late.sessionKeeper.CreateGrantOnBehalfOf(ctx, commonsModuleAddr, sessionMsg)
	if err != nil {
		return nil, err
	}

	// 8. No legacy commons-side write. The grant is the canonical
	// record; queries (M9) project from session.Grants.
	//
	// No legacy `recurring_spend_scheduled` event — session emits
	// `grant_created` with `source=module_bypass` and `caller_module`
	// attributes (see x/session/keeper/public_api.go).

	return &types.MsgScheduleRecurringSpendResponse{Id: grantID}, nil
}
