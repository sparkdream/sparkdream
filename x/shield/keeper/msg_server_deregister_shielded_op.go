package keeper

import (
	"bytes"
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

func (k msgServer) DeregisterShieldedOp(ctx context.Context, msg *types.MsgDeregisterShieldedOp) (*types.MsgDeregisterShieldedOpResponse, error) {
	authority, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}
	if !bytes.Equal(k.GetAuthority(), authority) {
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", expectedAuthorityStr, msg.Authority)
	}

	if msg.MessageTypeUrl == "" {
		return nil, errorsmod.Wrap(types.ErrInvalidInnerMessage, "message_type_url cannot be empty")
	}

	// Verify the operation exists before removing
	if _, found := k.GetShieldedOp(ctx, msg.MessageTypeUrl); !found {
		return nil, types.ErrUnregisteredOperation
	}

	if err := k.DeleteShieldedOp(ctx, msg.MessageTypeUrl); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldedOpDeregistered,
		sdk.NewAttribute(types.AttributeKeyMessageType, msg.MessageTypeUrl),
	))

	return &types.MsgDeregisterShieldedOpResponse{}, nil
}
