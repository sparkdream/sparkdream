package keeper

import (
	"bytes"
	"context"

	"sparkdream/x/ecosystem/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) Spend(goCtx context.Context, msg *types.MsgSpend) (*types.MsgSpendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Authority Check (Gov Module) — compare bytes via the address codec to
	// avoid any bech32-string normalization drift.
	authorityBytes, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid authority address: %s", err)
	}
	if !bytes.Equal(authorityBytes, k.GetAuthority()) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid authority: got %s", msg.Authority)
	}

	// 2. Validate Amount
	if !msg.Amount.IsValid() {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidCoins, msg.Amount.String())
	}
	if !msg.Amount.IsAllPositive() {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidCoins, "amount must be positive")
	}

	// 3. Validate Recipient
	recipient, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid recipient address: %s", err)
	}

	// 3. Transfer Funds
	err = k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, msg.Amount)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "failed to send coins from ecosystem module")
	}

	// 4. Emit Event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"ecosystem_spend",
			sdk.NewAttribute("recipient", msg.Recipient),
			sdk.NewAttribute("amount", msg.Amount.String()),
		),
	)

	return &types.MsgSpendResponse{}, nil
}
