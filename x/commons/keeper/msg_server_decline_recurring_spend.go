package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"sparkdream/x/commons/types"
)

// DeclineRecurringSpend lets the recipient permanently opt out of a
// schedule — e.g. when leaving a role. After decline, the schedule moves
// to RECIPIENT_DECLINED and no further claims succeed. The council can
// always re-schedule (with a new id) if it wants to designate someone
// else.
func (k msgServer) DeclineRecurringSpend(goCtx context.Context, msg *types.MsgDeclineRecurringSpend) (*types.MsgDeclineRecurringSpendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if _, err := k.addressCodec.StringToBytes(msg.Recipient); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid recipient address")
	}

	rs, err := k.GetRecurringSpend(ctx, msg.Id)
	if err != nil {
		return nil, err
	}

	if rs.Recipient != msg.Recipient {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendUnauthorized,
			"caller %s is not the schedule's recipient %s", msg.Recipient, rs.Recipient)
	}

	if rs.Status != types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrRecurringSpendInactive,
			"schedule %d is in status %s", rs.Id, rs.Status)
	}

	rs.Status = types.RecurringSpendStatus_RECURRING_SPEND_STATUS_RECIPIENT_DECLINED
	if err := k.RecurringSpends.Set(ctx, rs.Id, rs); err != nil {
		return nil, errorsmod.Wrap(err, "persist decline")
	}
	if err := k.decActiveCount(ctx, rs.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "drop active count")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"recurring_spend_declined",
			sdk.NewAttribute("id", fmt.Sprintf("%d", rs.Id)),
			sdk.NewAttribute("authority", rs.Authority),
			sdk.NewAttribute("recipient", rs.Recipient),
		),
	)

	return &types.MsgDeclineRecurringSpendResponse{}, nil
}
