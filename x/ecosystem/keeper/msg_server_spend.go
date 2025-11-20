package keeper

import (
	"context"

	"sparkdream/x/ecosystem/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) Spend(goCtx context.Context, msg *types.MsgSpend) (*types.MsgSpendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Authority Check (Gov Module)
	// Convert the Keeper's stored bytes into a Bech32 string
	expectedAuthority := sdk.AccAddress(k.GetAuthority()).String()
	if expectedAuthority != msg.Authority {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid authority: expected %s, got %s", k.GetAuthority(), msg.Authority)
	}

	// 2. Validate Recipient
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
