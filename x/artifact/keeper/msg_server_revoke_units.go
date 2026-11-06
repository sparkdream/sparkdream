package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// RevokeUnits burns a holder's free units as the class owner, when the
// class's burn_authorization lets the issuer burn. The record deposit and
// unclaimed rewards of an emptied balance go to the holder, never the
// revoker.
func (k msgServer) RevokeUnits(ctx context.Context, msg *types.MsgRevokeUnits) (*types.MsgRevokeUnitsResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	if err := types.ValidateText("reason", msg.Reason, types.RevokeReasonMaxLength, false); err != nil {
		return nil, err
	}
	c, err := k.getEditionsClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if !k.canActAs(ctx, msg.Owner, c.Owner) {
		return nil, types.ErrNotClassOwner
	}
	if !c.Flags.IssuerMayBurn() {
		return nil, types.ErrNotRevocable
	}
	if msg.Amount == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidBatch, "amount must be positive")
	}
	e, err := k.getEdition(ctx, c.Id, msg.EditionId)
	if err != nil {
		return nil, err
	}
	// The holder may have migrated its key since the units were credited:
	// the balance stays keyed by the old address, so resolve it through the
	// alias contract (either address may be named). Only free units are
	// revoked; a revocable class is never transferable, so its units can
	// never be listed or pending a transfer.
	holder := k.unitsHolderOf(ctx, c.Id, e.Id, msg.Holder)
	if err := k.debit(ctx, &e, holder, msg.Amount, false); err != nil {
		return nil, err
	}
	e.Supply -= msg.Amount
	if e.Burned, err = types.AddUnits(e.Burned, msg.Amount); err != nil {
		return nil, err
	}
	if err := k.saveEdition(ctx, e); err != nil {
		return nil, err
	}
	emit(ctx, types.EventUnitsRevoked, classAttr(c.Id), editionAttr(e.Id),
		sdk.NewAttribute("holder", holder), sdk.NewAttribute("amount", u64(msg.Amount)), sdk.NewAttribute("reason", msg.Reason))
	return &types.MsgRevokeUnitsResponse{}, nil
}
