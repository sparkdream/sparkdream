package keeper

import (
	"context"
	"strings"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
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
	if record.Owner != msg.Authority {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "you do not own name '%s'", name)
	}

	target := strings.TrimSpace(msg.Target)
	if target != "" {
		if _, err := sdk.AccAddressFromBech32(target); err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "target is not a valid bech32 address")
		}
	}

	// Revoke previous acceptance if there was one.
	if record.Target != "" && record.TargetAccepted {
		if err := k.AcceptedTargets.Remove(ctx, collections.Join(record.Target, name)); err != nil {
			return nil, err
		}
		// If the previous target had this name as their primary, clear it —
		// reverse resolution must reflect current consent.
		prevInfo, err := k.Owners.Get(ctx, record.Target)
		if err == nil && prevInfo.PrimaryName == name {
			prevInfo.PrimaryName = ""
			if err := k.Owners.Set(ctx, record.Target, prevInfo); err != nil {
				return nil, err
			}
		}
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
