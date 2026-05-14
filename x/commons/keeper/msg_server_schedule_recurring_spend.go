package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"sparkdream/x/commons/types"
)

// ScheduleRecurringSpend records a council-approved recurring disbursement.
// The signer (authority) must be a registered council policy address;
// callers are expected to wrap this in a MsgSubmitProposal so it executes
// only after a council vote.
//
// Schedule creation does NOT itself spend SPARK or touch EpochSpending —
// only claims do. This keeps the cost of "set up monthly payment for the
// next year" identical to the cost of a single SpendFromCommons, while the
// resulting commitment is auditable and cancelable.
func (k msgServer) ScheduleRecurringSpend(goCtx context.Context, msg *types.MsgScheduleRecurringSpend) (*types.MsgScheduleRecurringSpendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Address sanity.
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid authority address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Recipient); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid recipient address")
	}

	// 2. Authority must be a registered group policy. Use the same lookup
	// as MsgSpendFromCommons so the two paths share gating semantics.
	if _, _, found := k.GetGroupByPolicy(ctx, msg.Authority); !found {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound,
			"signer %s is not a registered group policy", msg.Authority)
	}

	// 3. Amount sanity.
	if !msg.AmountPerPeriod.IsValid() || msg.AmountPerPeriod.IsZero() {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "amount_per_period must be valid non-zero coins")
	}

	// 4. Period / window sanity, gated by params.
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "load params")
	}
	if msg.PeriodSeconds < params.MinRecurringPeriodSeconds {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendInvalidPeriod,
			"period_seconds %d < min %d", msg.PeriodSeconds, params.MinRecurringPeriodSeconds)
	}

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
	if msg.EndTime-start > params.MaxRecurringDurationSeconds {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendInvalidWindow,
			"duration %d exceeds cap %d", msg.EndTime-start, params.MaxRecurringDurationSeconds)
	}
	if msg.EndTime-start < msg.PeriodSeconds {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendInvalidWindow,
			"window (%d s) shorter than one period (%d s) — no claim could ever succeed",
			msg.EndTime-start, msg.PeriodSeconds)
	}

	// 5. Note bound.
	if len(msg.Note) > MaxRecurringSpendNoteLen {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"note exceeds %d chars (got %d)", MaxRecurringSpendNoteLen, len(msg.Note))
	}

	// 6. Per-authority active-schedule cap. Bumped before write so a
	// concurrent Schedule that would breach the ceiling is rejected.
	newCount, err := k.incActiveCount(ctx, msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "bump active count")
	}
	if newCount > params.MaxActiveRecurringSpendsPerGroup {
		// Roll back the optimistic bump.
		_ = k.decActiveCount(ctx, msg.Authority)
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendCapReached,
			"authority %s already has %d active schedules (max %d)",
			msg.Authority, newCount-1, params.MaxActiveRecurringSpendsPerGroup)
	}

	// 7. Allocate ID and persist.
	id, err := k.RecurringSpendSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "allocate id")
	}
	id++ // 1-indexed for human-friendly references in proposals/events.

	rs := types.RecurringSpend{
		Id:               id,
		Authority:        msg.Authority,
		Recipient:        msg.Recipient,
		AmountPerPeriod:  msg.AmountPerPeriod,
		PeriodSeconds:    msg.PeriodSeconds,
		StartTime:        start,
		EndTime:          msg.EndTime,
		LastClaimAdvance: start, // first claim is allowed at start + period.
		ClaimsMade:       0,
		Status:           types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE,
		Note:             msg.Note,
	}
	if err := k.setRecurringSpend(ctx, rs); err != nil {
		_ = k.decActiveCount(ctx, msg.Authority)
		return nil, errorsmod.Wrap(err, "persist schedule")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"recurring_spend_scheduled",
			sdk.NewAttribute("id", fmt.Sprintf("%d", rs.Id)),
			sdk.NewAttribute("authority", rs.Authority),
			sdk.NewAttribute("recipient", rs.Recipient),
			sdk.NewAttribute("period_seconds", fmt.Sprintf("%d", rs.PeriodSeconds)),
			sdk.NewAttribute("start_time", fmt.Sprintf("%d", rs.StartTime)),
			sdk.NewAttribute("end_time", fmt.Sprintf("%d", rs.EndTime)),
		),
	)

	return &types.MsgScheduleRecurringSpendResponse{Id: id}, nil
}
