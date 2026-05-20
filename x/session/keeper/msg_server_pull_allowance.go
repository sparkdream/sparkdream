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

// PullAllowance withdraws `amount` from a SPENDING_ALLOWANCE grant
// budget to `recipient`.
//
// Order of operations (per Rev 2 §4.3):
//  1. Authorization checks — grant exists, type matches, status is
//     ACTIVE or PAUSED_INSUFFICIENT_FUNDS, caller is the recorded
//     grantee, recipient != granter, denom matches grant.denom (also
//     in params.allowed_denoms), amount >= params.min_pull_amount,
//     recipient in allowed_recipients (or list empty).
//  2. If the rolling window has rolled over, reset
//     current_period_start = block_time and spent_in_current_period = 0.
//     The reset only commits if the rest of the pull succeeds — we do
//     it in-memory first, persist at the end. This prevents a
//     malicious recipient from triggering a no-op reset via repeated
//     invalid calls.
//  3. Per-period budget check.
//  4. Bank send. Failure -> PAUSED_INSUFFICIENT_FUNDS.
//  5. spent_in_current_period += amount; flip back to ACTIVE if it was
//     PAUSED.
func (k msgServer) PullAllowance(ctx context.Context, msg *types.MsgPullAllowance) (*types.MsgPullAllowanceResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	grant, err := k.Grants.Get(ctx, msg.GrantId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(types.ErrGrantNotFound, "id=%d", msg.GrantId)
		}
		return nil, err
	}
	if grant.Type != types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE {
		return nil, errorsmod.Wrapf(types.ErrGrantTypeMismatch,
			"grant %d is %s, expected SPENDING_ALLOWANCE", grant.Id, grant.Type)
	}
	sa := grant.GetSpendingAllowance()
	if sa == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidPayload, "spending_allowance payload missing")
	}

	if grant.Grantee != msg.Grantee {
		return nil, errorsmod.Wrapf(types.ErrAllowanceUnauthorized,
			"caller %s is not the grantee %s", msg.Grantee, grant.Grantee)
	}

	switch grant.Status {
	case types.GrantStatus_GRANT_STATUS_ACTIVE,
		types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS:
	default:
		return nil, errorsmod.Wrapf(types.ErrGrantInactive,
			"grant %d is in status %s", grant.Id, grant.Status)
	}

	// Anti self-roundtrip.
	if msg.Recipient == grant.Granter {
		return nil, types.ErrAllowanceRecipientIsGranter
	}

	// Coin sanity + denom.
	if !msg.Amount.IsValid() || !msg.Amount.IsPositive() {
		return nil, types.ErrAmountNotPositive
	}
	if msg.Amount.Denom == "dream" {
		return nil, types.ErrDreamDenomForbidden
	}
	if !denomAllowed(params.AllowedDenoms, msg.Amount.Denom) {
		return nil, types.ErrDenomNotAllowed.Wrapf("denom: %s", msg.Amount.Denom)
	}
	if msg.Amount.Denom != sa.Denom {
		return nil, types.ErrAllowanceDenomMismatch.Wrapf("amount denom %q vs grant denom %q",
			msg.Amount.Denom, sa.Denom)
	}

	// Minimum pull amount.
	minPull, ok := sdkmath.NewIntFromString(params.MinPullAmount)
	if !ok || minPull.IsNegative() {
		return nil, types.ErrInvalidMinPullAmount
	}
	if msg.Amount.Amount.LT(minPull) {
		return nil, types.ErrAllowanceAmountBelowMin.Wrapf("amount=%s min=%s", msg.Amount.Amount, minPull)
	}

	// Recipient whitelist (empty list = unrestricted).
	if len(sa.AllowedRecipients) > 0 {
		match := false
		for _, r := range sa.AllowedRecipients {
			if r == msg.Recipient {
				match = true
				break
			}
		}
		if !match {
			return nil, errorsmod.Wrapf(types.ErrRecipientNotWhitelisted, "recipient: %s", msg.Recipient)
		}
	}

	// Compute the rolling-window state in memory; only persist if the
	// entire pull succeeds (so a failed bank send doesn't accidentally
	// reset the period clock).
	now := sdkCtx.BlockTime().Unix()
	periodStart := sa.CurrentPeriodStart
	spent := sa.SpentInCurrentPeriod
	if now >= periodStart+sa.PeriodSeconds {
		// Roll over to a fresh window.
		periodStart = now
		spent = sdk.NewCoin(sa.Denom, sdkmath.ZeroInt())
	}

	// Per-period budget check.
	newSpent := spent.Add(msg.Amount)
	if newSpent.Amount.GT(sa.MaxPerPeriod.Amount) {
		return nil, errorsmod.Wrapf(types.ErrAllowanceBudgetExceeded,
			"would-be spent %s > max_per_period %s", newSpent.Amount, sa.MaxPerPeriod.Amount)
	}

	// Grant-claim hooks — PreCheck pass. Downstream modules can veto
	// the pull here. PostCommit fires after the period-clock commit
	// below.
	if err := k.invokePreCheckHooks(ctx, grant, sdk.NewCoins(msg.Amount)); err != nil {
		return nil, err
	}

	// Bank send. Failure → PAUSED_INSUFFICIENT_FUNDS.
	granterAddr, err := k.addressCodec.StringToBytes(grant.Granter)
	if err != nil {
		return nil, err
	}
	recipientAddr, err := k.addressCodec.StringToBytes(msg.Recipient)
	if err != nil {
		return nil, err
	}
	if err := k.bankKeeper.SendCoins(ctx, granterAddr, recipientAddr, sdk.NewCoins(msg.Amount)); err != nil {
		previousStatus := grant.Status
		grant.Status = types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS
		// Do NOT commit the period reset on failure.
		grant.Payload = &types.Grant_SpendingAllowance{SpendingAllowance: sa}
		if persistErr := k.Grants.Set(ctx, grant.Id, grant); persistErr != nil {
			return nil, persistErr
		}
		if previousStatus == types.GrantStatus_GRANT_STATUS_ACTIVE {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"grant_paused_underfunded",
				sdk.NewAttribute("id", fmt.Sprintf("%d", grant.Id)),
				sdk.NewAttribute("type", grant.Type.String()),
				sdk.NewAttribute("granter", grant.Granter),
				sdk.NewAttribute("grantee", grant.Grantee),
				sdk.NewAttribute("attempted_amount", msg.Amount.String()),
			))
		}
		return nil, errorsmod.Wrap(types.ErrInsufficientGranterBalance, err.Error())
	}

	// Bank send succeeded — commit period clock + spent_in_current_period
	// and flip back to ACTIVE if previously paused.
	wasPaused := grant.Status == types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS
	sa.CurrentPeriodStart = periodStart
	sa.SpentInCurrentPeriod = newSpent
	grant.Payload = &types.Grant_SpendingAllowance{SpendingAllowance: sa}
	grant.Status = types.GrantStatus_GRANT_STATUS_ACTIVE
	if err := k.Grants.Set(ctx, grant.Id, grant); err != nil {
		return nil, err
	}

	// Grant-claim hooks — PostCommit pass. State-mutating side effects
	// atomic with the disbursement. Errors halt the tx; SDK rolls back
	// the bank send + grant update.
	if err := k.invokePostCommitHooks(ctx, grant, sdk.NewCoins(msg.Amount)); err != nil {
		return nil, err
	}

	if wasPaused {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"grant_resumed",
			sdk.NewAttribute("id", fmt.Sprintf("%d", grant.Id)),
			sdk.NewAttribute("type", grant.Type.String()),
			sdk.NewAttribute("granter", grant.Granter),
			sdk.NewAttribute("grantee", grant.Grantee),
		))
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"allowance_pulled",
		sdk.NewAttribute("grant_id", fmt.Sprintf("%d", grant.Id)),
		sdk.NewAttribute("granter", grant.Granter),
		sdk.NewAttribute("grantee", grant.Grantee),
		sdk.NewAttribute("recipient", msg.Recipient),
		sdk.NewAttribute("amount", msg.Amount.String()),
		sdk.NewAttribute("spent_in_period", sa.SpentInCurrentPeriod.String()),
		sdk.NewAttribute("max_per_period", sa.MaxPerPeriod.String()),
	))

	return &types.MsgPullAllowanceResponse{
		Transferred:    msg.Amount,
		SpentInPeriod:  sa.SpentInCurrentPeriod,
	}, nil
}
