package keeper

import (
	"context"
	"strings"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// SetTarget points (or unpoints) a name at a resolver target address.
// Setting always clears acceptance — the target must re-consent via
// MsgAcceptTarget. If the previous target had this name as their primary,
// that primary is cleared.
func (k msgServer) SetTarget(goCtx context.Context, msg *types.MsgSetTarget) (*types.MsgSetTargetResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	name := strings.ToLower(strings.TrimSpace(msg.Name))
	record, found := k.GetName(ctx, name)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrNameNotFound, "name '%s' not found", name)
	}
	if !k.canActAs(ctx, msg.Authority, record.Owner) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "you do not own name '%s'", name)
	}

	target := strings.TrimSpace(msg.Target)
	if target != "" {
		if _, err := sdk.AccAddressFromBech32(target); err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "target is not a valid bech32 address")
		}
	}

	// Revoke previous acceptance if there was one; reverse resolution must
	// reflect current consent.
	if err := k.clearTargetConsent(ctx, record); err != nil {
		return nil, err
	}

	record.Target = target
	record.TargetAccepted = false
	if err := k.SetName(ctx, record); err != nil {
		return nil, err
	}

	if err := k.RecordOwnerActivity(ctx, msg.Authority); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent("name_target_set",
			sdk.NewAttribute("name", name),
			sdk.NewAttribute("owner", msg.Authority),
			sdk.NewAttribute("target", target),
		),
	)

	return &types.MsgSetTargetResponse{}, nil
}
