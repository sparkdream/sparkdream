package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"sparkdream/x/commons/types"
)

func (k msgServer) SpendFromCommons(ctx context.Context, msg *types.MsgSpendFromCommons) (*types.MsgSpendFromCommonsResponse, error) {
	// 1. Get the Authority Address (The Group Policy Address)
	// We need the byte representation for SendCoins
	authorityAddr, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// 2. SECURITY CHECK: Verify the Signer is the Authorized Commons Council
	// We fetch the address stored in params to ensure random people aren't calling this.
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	if msg.Authority != params.CommonsCouncilAddress {
		return nil, errorsmod.Wrapf(
			sdkerrors.ErrUnauthorized,
			"signer %s is not the authorized Commons Council policy account",
			msg.Authority,
		)
	}

	// 3. Validate Recipient Address
	recipientAddr, err := k.addressCodec.StringToBytes(msg.Recipient)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid recipient address")
	}

	// 4. Execute the Spend
	// Logic: Group Policy (Authority) -> Recipient
	// We use SendCoins because Group Policies are standard accounts, not Module Accounts.
	err = k.bankKeeper.SendCoins(
		ctx,
		authorityAddr, // From: Commons Council (Group Policy)
		recipientAddr, // To: Recipient
		msg.Amount,    // Now this is directly sdk.Coins
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to transfer funds from commons council")
	}

	return &types.MsgSpendFromCommonsResponse{}, nil
}
