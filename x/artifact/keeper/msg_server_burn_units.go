package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// BurnUnits destroys free units the signer holds. Emptying the balance
// refunds its record deposit and pays out unclaimed rewards.
func (k msgServer) BurnUnits(ctx context.Context, msg *types.MsgBurnUnits) (*types.MsgBurnUnitsResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getEditionsClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if !c.Flags.HolderMayBurn() {
		return nil, types.ErrHolderBurnNotAllowed
	}
	if msg.Amount == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidBatch, "amount must be positive")
	}
	e, err := k.getEdition(ctx, c.Id, msg.EditionId)
	if err != nil {
		return nil, err
	}
	holder := k.balanceHolder(ctx, c.Id, e.Id, msg.Owner)
	if err := k.debit(ctx, &e, holder, msg.Amount, false); err != nil {
		return nil, err
	}
	e.Supply -= msg.Amount
	e.Burned += msg.Amount
	if err := k.saveEdition(ctx, e); err != nil {
		return nil, err
	}
	emit(ctx, types.EventUnitsBurned, classAttr(c.Id), editionAttr(e.Id),
		sdk.NewAttribute("owner", msg.Owner), sdk.NewAttribute("amount", u64(msg.Amount)))
	return &types.MsgBurnUnitsResponse{}, nil
}
