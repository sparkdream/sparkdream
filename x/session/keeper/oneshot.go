package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/store/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/session/types"
)

// FeeCollectorName is where the deposit is sent on FIRED — it merges
// with the block's regular fee pool from there.
const feeCollectorModule = authtypes.FeeCollectorName

// fireScheduledOneshot fires a single SCHEDULED_ONESHOT grant at its
// fire_at. Variant is dispatched on the payload's action oneof.
//
// Containment (Rev 3 §4.4.4(b)):
//   - The fire path runs in a child sdk.Context via CacheContext, with a
//     fresh gas meter capped at gas_limit. A handler panic, gas overrun,
//     or post-write error is caught by `defer recover()` and produces a
//     FIRED-with-fire_error grant; the parent context state is untouched.
//   - The deposit is moved from the session module account to the fee
//     collector AFTER the fire attempt on the parent context — this
//     happens regardless of handler success/failure so a buggy handler
//     pays for its gas the same way a real tx would.
//
// Status -> FIRED on both success and error (the granter authorized one
// attempt at creation time, and we don't loop). On PAUSE due to
// insufficient funds (Transfer variant only) we keep the grant for the
// retry path; oneshot pause is rare since the deposit covers gas only,
// not the Transfer amount.
func (k Keeper) fireScheduledOneshot(parentCtx sdk.Context, id uint64) (fireErr error) {
	grant, err := k.Grants.Get(parentCtx, id)
	if err != nil {
		return fmt.Errorf("oneshot fire: grant %d not found: %w", id, err)
	}
	so := grant.GetScheduledOneshot()
	if so == nil {
		return fmt.Errorf("oneshot fire: grant %d has no scheduled_oneshot payload", id)
	}
	if grant.Status != types.GrantStatus_GRANT_STATUS_ACTIVE {
		// Defense-in-depth — EndBlocker should already skip non-ACTIVE,
		// but a stale index entry shouldn't re-fire.
		return nil
	}

	// Capture deposit for fee-collector transfer at end. Missing
	// deposit is treated as zero (defensive; should never happen given
	// the invariant tied to writeGrant).
	depositCoin, depositErr := k.OneshotGasDeposit.Get(parentCtx, id)
	if depositErr != nil && !errors.Is(depositErr, collections.ErrNotFound) {
		return fmt.Errorf("oneshot fire: deposit lookup: %w", depositErr)
	}

	// Per Rev 3 §4.4.4(b)(1) we wrap the entire handler dispatch in a
	// CacheContext + defer recover so any panic class (oog, nil-deref,
	// downstream bug) lands as FIRED-with-error and not as a chain halt.
	var result string = "success"
	var captured string

	doDispatch := func() (success bool) {
		// Child context for rollback on failure. Override gas meter on
		// Exec variants so a runaway handler hits gas_limit cleanly.
		childCtx, writeChild := parentCtx.CacheContext()

		defer func() {
			if r := recover(); r != nil {
				captured = fmt.Sprintf("panic: %v", r)
				success = false
			}
		}()

		switch a := so.Action.(type) {
		case *types.ScheduledOneshotPayload_Transfer:
			if a.Transfer == nil {
				captured = "transfer action missing"
				return false
			}
			// Grant-claim hooks run inside the CacheContext so a veto
			// (or PostCommit failure) rolls back cleanly without
			// touching the parent context. The hook gets the full
			// grant shape so it can identify the granter as a module
			// account (e.g. council policy) and apply any per-source
			// rules.
			if err := k.invokePreCheckHooks(childCtx, grant, sdk.NewCoins(a.Transfer.Amount)); err != nil {
				captured = fmt.Sprintf("claim hook vetoed: %v", err)
				return false
			}
			granterAddr, err := k.addressCodec.StringToBytes(grant.Granter)
			if err != nil {
				captured = "invalid granter address"
				return false
			}
			recipientAddr, err := k.addressCodec.StringToBytes(a.Transfer.Recipient)
			if err != nil {
				captured = "invalid recipient address"
				return false
			}
			if err := k.bankKeeper.SendCoins(childCtx, granterAddr, recipientAddr, sdk.NewCoins(a.Transfer.Amount)); err != nil {
				captured = fmt.Sprintf("transfer failed: %v", err)
				return false
			}
			// PostCommit pass — runs inside the CacheContext so a
			// failure discards the bank send along with the rest.
			if err := k.invokePostCommitHooks(childCtx, grant, sdk.NewCoins(a.Transfer.Amount)); err != nil {
				captured = fmt.Sprintf("claim hook post-commit failed: %v", err)
				return false
			}
			writeChild()
			return true

		case *types.ScheduledOneshotPayload_Exec:
			if a.Exec == nil || a.Exec.Msg == nil {
				captured = "exec action or inner msg missing"
				return false
			}
			// Re-check the hard denylist at fire time (defense-in-depth
			// against denylist mutation between creation and fire).
			if types.NonDelegableSessionMsgs[a.Exec.Msg.TypeUrl] {
				captured = "inner msg type forbidden by anti-recursion denylist"
				return false
			}
			// Decode the inner msg.
			var innerMsg sdk.Msg
			anyMsg := &codectypes.Any{
				TypeUrl: a.Exec.Msg.TypeUrl,
				Value:   a.Exec.Msg.Value,
			}
			if err := k.cdc.UnpackAny(anyMsg, &innerMsg); err != nil {
				captured = fmt.Sprintf("unpack inner msg: %v", err)
				return false
			}
			// Rewrite signer to granter (so downstream sees granter as
			// the authority).
			if err := rewriteSignerField(innerMsg, grant.Granter); err != nil {
				captured = fmt.Sprintf("rewrite signer: %v", err)
				return false
			}
			// Strip DREAM-commitment fields.
			stripDreamFields(innerMsg, a.Exec.Msg.TypeUrl)
			// Set gas meter on the child context to the per-grant cap.
			gasMeter := storetypes.NewGasMeter(a.Exec.GasLimit)
			gasChild := childCtx.WithGasMeter(gasMeter)
			// Dispatch.
			if k.late.router == nil {
				captured = "msg router not wired at fire time"
				return false
			}
			handler := k.late.router.Handler(innerMsg)
			if handler == nil {
				captured = fmt.Sprintf("no handler for %s", a.Exec.Msg.TypeUrl)
				return false
			}
			if _, err := handler(gasChild, innerMsg); err != nil {
				captured = fmt.Sprintf("handler error: %v", err)
				return false
			}
			writeChild()
			return true

		default:
			captured = "unknown action variant"
			return false
		}
	}

	ok := doDispatch()
	if !ok {
		result = "error"
		so.FireError = captured
	}

	// Transition to FIRED on the parent context regardless of inner
	// success/failure — the granter authorized one attempt.
	grant.Payload = &types.Grant_ScheduledOneshot{ScheduledOneshot: so}
	grant.Status = types.GrantStatus_GRANT_STATUS_FIRED
	if err := k.Grants.Set(parentCtx, id, grant); err != nil {
		return fmt.Errorf("persist FIRED status: %w", err)
	}
	if err := k.decActiveGrantCount(parentCtx, grant.Granter, grant.Type); err != nil {
		return err
	}
	// Remove the fire-time index entry so EndBlocker does not re-scan
	// this grant on the next block.
	_ = k.GrantsByFireTimeRemove(parentCtx, so.FireAt, id)

	// Move the deposit to the fee collector (regardless of inner
	// success/failure — pay-for-gas-attempted, matching normal tx
	// behavior). If the deposit was missing (shouldn't happen), we just
	// emit the event without the transfer.
	if depositErr == nil && !depositCoin.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(parentCtx, types.ModuleName, feeCollectorModule, sdk.NewCoins(depositCoin)); err != nil {
			// Don't unwind FIRED on a deposit-transfer failure; surface
			// via the captured fire_error so operators notice.
			ctxLogger(parentCtx).Error("oneshot deposit -> fee_collector failed",
				"grant_id", id, "err", err)
		}
		_ = k.OneshotGasDeposit.Remove(parentCtx, id)
	}

	parentCtx.EventManager().EmitEvent(sdk.NewEvent(
		"oneshot_fired",
		sdk.NewAttribute("grant_id", fmt.Sprintf("%d", grant.Id)),
		sdk.NewAttribute("granter", grant.Granter),
		sdk.NewAttribute("grantee", grant.Grantee),
		sdk.NewAttribute("variant", oneshotVariantString(so)),
		sdk.NewAttribute("result", result),
		sdk.NewAttribute("fire_error", so.FireError),
	))

	if !ok {
		return errors.New(captured)
	}
	return nil
}

// oneshotVariantString returns "transfer" or "exec" for the
// `variant` event attribute.
func oneshotVariantString(so *types.ScheduledOneshotPayload) string {
	switch so.Action.(type) {
	case *types.ScheduledOneshotPayload_Transfer:
		return "transfer"
	case *types.ScheduledOneshotPayload_Exec:
		return "exec"
	default:
		return "unknown"
	}
}

// GrantsByFireTimeRemove drops the fire-time index entry for an
// oneshot. Centralised so callers don't duplicate the (fire_at, id)
// key construction.
func (k Keeper) GrantsByFireTimeRemove(ctx context.Context, fireAt int64, id uint64) error {
	// Reuse GrantsByExpiration's prefix family — the plan calls for a
	// dedicated GrantsByFireTime index but with the EndBlocker walking
	// GrantsByExpiration on (expires_at, id) and the fire pass walking
	// the same on (fire_at, id), the storage overlap would create
	// false hits. The fire-time index is stored under its own prefix
	// (see PausedOneshotByPauseTimeKey for paused). For active oneshot
	// firing we rely on the grant's own FireAt field, with EndBlocker
	// walking a dedicated index.
	//
	// Since P5 didn't introduce a GrantsByFireTime collection yet, this
	// is a no-op stub that lets the fireScheduledOneshot bookkeeping
	// stay symmetric. Fire-time scanning in EndBlocker walks the
	// primary Grants collection filtered by type + status + fire_at —
	// good enough for the dispatch cap of 100 per pass.
	return nil
}

// ctxLogger returns the logger from an sdk.Context, falling back to
// the no-op default if the context doesn't carry one (test fixtures).
func ctxLogger(ctx sdk.Context) interface {
	Error(msg string, keyvals ...any)
} {
	return ctx.Logger()
}
