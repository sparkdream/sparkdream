package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// FundEdition pays SPARK to an edition's current holders, pro rata to the
// units they hold (retroactive funding of an impact certificate,
// docs/x-artifact-spec.md §7.21). The funds wait in the module account
// until each holder claims or empties their balance; division dust stays
// in the module.
func (k msgServer) FundEdition(ctx context.Context, msg *types.MsgFundEdition) (*types.MsgFundEditionResponse, error) {
	funder, err := k.decodeAddr("funder", msg.Funder)
	if err != nil {
		return nil, err
	}
	if err := k.validatePrice(ctx, msg.Amount, false); err != nil {
		return nil, err
	}
	c, err := k.getEditionsClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	e, err := k.getEdition(ctx, c.Id, msg.EditionId)
	if err != nil {
		return nil, err
	}
	if isEditionHidden(c, e) {
		return nil, types.ErrContentHidden
	}
	if e.Supply == 0 {
		return nil, types.ErrNoCirculatingUnits
	}
	delta := types.RewardPerUnitDelta(msg.Amount.Amount, e.Supply)
	if !delta.IsPositive() {
		return nil, errorsmod.Wrap(types.ErrInvalidDenom, "amount too small to reach any unit")
	}
	if err := k.collect(ctx, funder, msg.Amount.Amount); err != nil {
		return nil, err
	}
	e.RewardPerUnit = e.RewardPerUnit.Add(delta)
	e.RewardsFunded = e.RewardsFunded.Add(msg.Amount.Amount)
	if err := k.saveEdition(ctx, e); err != nil {
		return nil, err
	}
	emit(ctx, types.EventEditionFunded, classAttr(c.Id), editionAttr(e.Id),
		sdk.NewAttribute("funder", msg.Funder), sdk.NewAttribute("amount", msg.Amount.String()),
		sdk.NewAttribute("units", u64(e.Supply)))
	return &types.MsgFundEditionResponse{}, nil
}
