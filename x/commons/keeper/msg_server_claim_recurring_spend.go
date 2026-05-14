package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"sparkdream/x/commons/types"
)

// ClaimRecurringSpend disburses one period of an active schedule.
//
// Each claim:
//   - verifies the schedule is ACTIVE,
//   - verifies the schedule's logical clock (last_claim_advance) is at
//     least one period in the past relative to block time,
//   - runs CheckSpendPreconditions (term/activation/rate-limit, shared
//     with MsgSpendFromCommons) — meaning a term-expired council's
//     recurring schedules auto-pause, and every claim still counts
//     against the per-epoch rate limit,
//   - transfers amount_per_period from the council policy account,
//   - advances last_claim_advance by exactly period_seconds (anchored to
//     the schedule, not block time), and
//   - flips status to COMPLETED if the next advance would exceed end_time.
//
// A recipient who delays can catch up by submitting multiple claim txs;
// each is rate-limited independently, so catch-up self-throttles to the
// per-epoch ceiling.
func (k msgServer) ClaimRecurringSpend(goCtx context.Context, msg *types.MsgClaimRecurringSpend) (*types.MsgClaimRecurringSpendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	recipientAddr, err := k.addressCodec.StringToBytes(msg.Recipient)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid recipient address")
	}

	rs, err := k.GetRecurringSpend(ctx, msg.Id)
	if err != nil {
		return nil, err
	}

	// Only the recipient on file can claim.
	if rs.Recipient != msg.Recipient {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendUnauthorized,
			"caller %s is not the schedule's recipient %s", msg.Recipient, rs.Recipient)
	}

	if rs.Status != types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendInactive,
			"schedule %d is in status %s", rs.Id, rs.Status)
	}

	now := ctx.BlockTime().Unix()
	// Next eligible claim sits at last_claim_advance + period_seconds. For
	// a never-claimed schedule last_claim_advance == start_time, so the
	// first claim is admissible at start_time + period_seconds. This is
	// deliberate: the council vote authorizes payment AFTER one period of
	// work, not immediately.
	nextEligible := rs.LastClaimAdvance + rs.PeriodSeconds
	if now < nextEligible {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendNotDue,
			"next claim window opens at %d (now=%d)", nextEligible, now)
	}
	if nextEligible > rs.EndTime {
		// Window already closed without a claim being submitted in time.
		// Flag-and-store so subsequent calls short-circuit.
		rs.Status = types.RecurringSpendStatus_RECURRING_SPEND_STATUS_COMPLETED
		if err := k.RecurringSpends.Set(ctx, rs.Id, rs); err != nil {
			return nil, errorsmod.Wrap(err, "persist completion")
		}
		_ = k.decActiveCount(ctx, rs.Authority)
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendWindowClosed,
			"schedule %d ended at %d", rs.Id, rs.EndTime)
	}

	authorityBytes, err := k.addressCodec.StringToBytes(rs.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "schedule has invalid authority address")
	}

	// Shared preconditions (activation, term-expiry, per-epoch rate
	// limit). Returning the rate-limit error here is the correct behavior
	// — the recipient can retry after the epoch rolls over.
	if err := k.CheckSpendPreconditions(ctx, rs.Authority, rs.AmountPerPeriod); err != nil {
		return nil, err
	}

	// Transfer from the council policy to the recipient.
	if err := k.bankKeeper.SendCoins(ctx, authorityBytes, recipientAddr, rs.AmountPerPeriod); err != nil {
		return nil, errorsmod.Wrap(err, "transfer failed")
	}

	// Advance the schedule's logical clock by exactly one period. If the
	// next advance would exceed end_time, mark the schedule COMPLETED so
	// future calls short-circuit before re-entering CheckSpendPreconditions.
	rs.LastClaimAdvance = nextEligible
	rs.ClaimsMade++
	if rs.LastClaimAdvance+rs.PeriodSeconds > rs.EndTime {
		rs.Status = types.RecurringSpendStatus_RECURRING_SPEND_STATUS_COMPLETED
		_ = k.decActiveCount(ctx, rs.Authority)
	}
	if err := k.RecurringSpends.Set(ctx, rs.Id, rs); err != nil {
		return nil, errorsmod.Wrap(err, "persist advance")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"recurring_spend_claimed",
			sdk.NewAttribute("id", fmt.Sprintf("%d", rs.Id)),
			sdk.NewAttribute("authority", rs.Authority),
			sdk.NewAttribute("recipient", rs.Recipient),
			sdk.NewAttribute("amount", rs.AmountPerPeriod.String()),
			sdk.NewAttribute("claim_number", fmt.Sprintf("%d", rs.ClaimsMade)),
			sdk.NewAttribute("last_claim_advance", fmt.Sprintf("%d", rs.LastClaimAdvance)),
		),
	)

	return &types.MsgClaimRecurringSpendResponse{
		ClaimNumber:   rs.ClaimsMade,
		NextClaimTime: rs.LastClaimAdvance + rs.PeriodSeconds,
	}, nil
}
