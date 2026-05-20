package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"sparkdream/x/commons/types"
)

func (k msgServer) SpendFromCommons(goCtx context.Context, msg *types.MsgSpendFromCommons) (*types.MsgSpendFromCommonsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Validate addresses.
	authorityAddr, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid authority address")
	}

	recipientAddr, err := k.addressCodec.StringToBytes(msg.Recipient)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid recipient address")
	}

	// 2. Activation / expiration / rate-limit gate. Atomically updates the
	// per-epoch counter so a follow-up call in the same epoch sees the new
	// total. The same EpochSpending bucket is debited by `SessionClaimHook`
	// (PreCheck + PostCommit) on every RECURRING_PULL claim whose granter
	// is a council policy, so a recurring schedule cannot bypass the same
	// constraints a one-off proposal must satisfy.
	if err := k.CheckSpendPreconditions(ctx, msg.Authority, msg.Amount); err != nil {
		return nil, err
	}

	// 3. Execute the transfer from the group policy account to the recipient.
	if err := k.bankKeeper.SendCoins(ctx, authorityAddr, recipientAddr, msg.Amount); err != nil {
		return nil, errorsmod.Wrap(err, "transfer failed")
	}

	return &types.MsgSpendFromCommonsResponse{}, nil
}
