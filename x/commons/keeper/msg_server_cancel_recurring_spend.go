package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"sparkdream/x/commons/types"
)

// CancelRecurringSpend marks an active schedule as CANCELED. The signer
// must be the same authority that created it — when wrapped in a
// MsgSubmitProposal this means the same council must vote to cancel.
//
// A canceled schedule rejects further claims but its row is retained for
// audit. Re-issuing a recurring spend requires a fresh Schedule call so
// each schedule has a distinct id and history.
func (k msgServer) CancelRecurringSpend(goCtx context.Context, msg *types.MsgCancelRecurringSpend) (*types.MsgCancelRecurringSpendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid authority address")
	}

	rs, err := k.GetRecurringSpend(ctx, msg.Id)
	if err != nil {
		return nil, err
	}

	// Only the original authority can cancel — symmetric with creation.
	if rs.Authority != msg.Authority {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendUnauthorized,
			"caller %s is not the schedule's authority %s", msg.Authority, rs.Authority)
	}

	// Cancelling a non-ACTIVE schedule is a no-op error: it gives the
	// caller a clear signal that nothing changed and the proposal that
	// wrapped it shouldn't have been needed.
	if rs.Status != types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendInactive,
			"schedule %d is in status %s", rs.Id, rs.Status)
	}

	rs.Status = types.RecurringSpendStatus_RECURRING_SPEND_STATUS_CANCELED
	if err := k.RecurringSpends.Set(ctx, rs.Id, rs); err != nil {
		return nil, errorsmod.Wrap(err, "persist cancel")
	}
	if err := k.decActiveCount(ctx, rs.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "drop active count")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"recurring_spend_canceled",
			sdk.NewAttribute("id", fmt.Sprintf("%d", rs.Id)),
			sdk.NewAttribute("authority", rs.Authority),
			sdk.NewAttribute("recipient", rs.Recipient),
		),
	)

	return &types.MsgCancelRecurringSpendResponse{}, nil
}
