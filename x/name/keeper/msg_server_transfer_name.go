package keeper

import (
	"context"
	"strings"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// TransferName moves ownership of a name from the current owner to a new
// owner. The recipient must be an active x/rep member, must not exceed the
// per-address name cap, and must not be the same address as the current
// owner. Rejected when the name has an active dispute (otherwise an owner
// could dump a disputed name to escape stake-burn). Existing target /
// target_accepted state carries over; the new owner can revoke acceptance by
// re-setting the target.
func (k msgServer) TransferName(goCtx context.Context, msg *types.MsgTransferName) (*types.MsgTransferNameResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	name := strings.ToLower(strings.TrimSpace(msg.Name))
	record, found := k.GetName(ctx, name)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrNameNotFound, "name '%s' not found", name)
	}
	if record.Owner != msg.Authority {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "you do not own name '%s'", name)
	}

	newOwnerAddr, err := sdk.AccAddressFromBech32(msg.NewOwner)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid new_owner address")
	}
	if msg.NewOwner == msg.Authority {
		return nil, types.ErrCannotTransferToSelf
	}

	// Active dispute? Refuse — otherwise the current owner can shed liability
	// by transferring the disputed name to a third party.
	if d, ok := k.GetDispute(ctx, name); ok && d.Active {
		return nil, errorsmod.Wrapf(types.ErrCannotTransferDisputed, "name '%s' has an active dispute", name)
	}

	// New owner must be an active x/rep member (same gate as registration).
	isMember, err := k.IsActiveRepMember(ctx, msg.NewOwner)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "membership check failed: %s", err.Error())
	}
	if !isMember {
		return nil, types.ErrRecipientNotMember
	}

	params := k.GetParams(ctx)
	count, err := k.GetOwnedNamesCount(ctx, newOwnerAddr)
	if err != nil {
		return nil, err
	}
	if count >= params.MaxNamesPerAddress {
		return nil, errorsmod.Wrapf(types.ErrTooManyNames, "recipient holds %d / %d names", count, params.MaxNamesPerAddress)
	}

	oldOwnerAddr, err := sdk.AccAddressFromBech32(record.Owner)
	if err != nil {
		return nil, err
	}

	// Move the name: update record, swap secondary indexes.
	record.Owner = msg.NewOwner
	if err := k.SetName(ctx, record); err != nil {
		return nil, err
	}
	if err := k.RemoveNameFromOwner(ctx, oldOwnerAddr, name); err != nil {
		return nil, err
	}
	if err := k.AddNameToOwner(ctx, newOwnerAddr, name); err != nil {
		return nil, err
	}

	// Clear the old owner's primary if it was this name — they no longer own
	// it, so reverse resolution must not point at a name they don't control.
	oldInfo, err := k.Owners.Get(ctx, msg.Authority)
	if err == nil && oldInfo.PrimaryName == name {
		oldInfo.PrimaryName = ""
		if err := k.Owners.Set(ctx, msg.Authority, oldInfo); err != nil {
			return nil, err
		}
	}

	// Refresh both parties' activity timestamps.
	if err := k.RecordOwnerActivity(ctx, msg.Authority); err != nil {
		return nil, err
	}
	if err := k.RecordOwnerActivity(ctx, msg.NewOwner); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent("name_transferred",
			sdk.NewAttribute("name", name),
			sdk.NewAttribute("old_owner", msg.Authority),
			sdk.NewAttribute("new_owner", msg.NewOwner),
		),
	)

	return &types.MsgTransferNameResponse{}, nil
}
