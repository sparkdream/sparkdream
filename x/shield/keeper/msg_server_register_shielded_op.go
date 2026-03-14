package keeper

import (
	"bytes"
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

func (k msgServer) RegisterShieldedOp(ctx context.Context, msg *types.MsgRegisterShieldedOp) (*types.MsgRegisterShieldedOpResponse, error) {
	authority, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}
	if !bytes.Equal(k.GetAuthority(), authority) {
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", expectedAuthorityStr, msg.Authority)
	}

	reg := msg.Registration
	if reg.MessageTypeUrl == "" {
		return nil, errorsmod.Wrap(types.ErrInvalidInnerMessage, "message_type_url cannot be empty")
	}

	if err := k.SetShieldedOp(ctx, reg); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldedOpRegistered,
		sdk.NewAttribute(types.AttributeKeyMessageType, reg.MessageTypeUrl),
	))

	return &types.MsgRegisterShieldedOpResponse{}, nil
}
