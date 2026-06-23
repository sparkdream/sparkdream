package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CancelUnbondRole cancels part or all of an in-flight unbond for the caller's
// role, returning the cancelled amount to active bond immediately — without
// waiting out the cooldown. This is the inverse of an incremental unbond: it
// shrinks pending_unbond_amount.
//
// No DREAM moves: a queued unbond never unlocks DREAM (pending is only an
// earmark on bond that stays locked in current_bond), so cancelling it simply
// removes the earmark. Cancelling the entire pending amount clears the
// completion clock and recomputes bond_status from the unchanged current_bond
// (the role returns to NORMAL / RECOVERY as its bond warrants). A partial
// cancel leaves the role UNBONDING with the remaining pending on its existing
// clock (reducing the amount never shortens a tail, so the clock is untouched).
func (k msgServer) CancelUnbondRole(ctx context.Context, msg *types.MsgCancelUnbondRole) (*types.MsgCancelUnbondRoleResponse, error) {
	if err := validateRoleType(msg.RoleType); err != nil {
		return nil, err
	}

	if _, err := sdk.AccAddressFromBech32(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	amount, ok := math.NewIntFromString(msg.Amount)
	if !ok || amount.IsNegative() || amount.IsZero() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid cancel amount")
	}

	key := bondedRoleKey(msg.RoleType, msg.Creator)
	br, err := k.BondedRoles.Get(ctx, key)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBondedRoleNotFound,
			"%s:%s", msg.RoleType.String(), msg.Creator)
	}

	if br.BondStatus != types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "no unbond in flight to cancel")
	}

	pending, err := parseIntOrZero(br.PendingUnbondAmount)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid pending_unbond_amount in bonded role record")
	}
	if amount.GT(pending) {
		return nil, errorsmod.Wrapf(types.ErrInvalidRequest,
			"cannot cancel %s, only %s pending", amount.String(), pending.String())
	}

	newPending := pending.Sub(amount)

	if newPending.IsZero() {
		// Full cancel: clear the unbond and return the role to active status.
		currentBond, err := parseIntOrZero(br.CurrentBond)
		if err != nil {
			return nil, errorsmod.Wrap(err, "invalid current_bond in bonded role record")
		}
		br.PendingUnbondAmount = "0"
		br.UnbondCompletionTime = 0
		br.BondStatus = k.computeBondStatus(ctx, msg.RoleType, currentBond)
	} else {
		// Partial cancel: shrink pending, keep the role UNBONDING on its
		// existing clock.
		br.PendingUnbondAmount = newPending.String()
	}

	if err := k.BondedRoles.Set(ctx, key, br); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store bonded role")
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			"bonded_role_unbond_cancelled",
			sdk.NewAttribute("role_type", msg.RoleType.String()),
			sdk.NewAttribute("address", msg.Creator),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("pending_unbond_amount", br.PendingUnbondAmount),
			sdk.NewAttribute("completion_time", fmt.Sprintf("%d", br.UnbondCompletionTime)),
			sdk.NewAttribute("bond_status", br.BondStatus.String()),
		),
	)

	return &types.MsgCancelUnbondRoleResponse{}, nil
}
