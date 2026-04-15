package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) TopUpBridgeStake(ctx context.Context, msg *types.MsgTopUpBridgeStake) (*types.MsgTopUpBridgeStakeResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Operator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid operator address")
	}

	// 1. Verify bridge exists and is ACTIVE or UNBONDING
	bridgeKey := collections.Join(msg.Operator, msg.PeerId)
	bridge, err := k.BridgeOperators.Get(ctx, bridgeKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not found for peer %s", msg.Operator, msg.PeerId)
	}
	if bridge.Status != types.BridgeStatus_BRIDGE_STATUS_ACTIVE &&
		bridge.Status != types.BridgeStatus_BRIDGE_STATUS_UNBONDING {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotActive, "bridge status is %s, cannot top up", bridge.Status)
	}

	// 2. Verify denom is uspark
	if msg.Amount.Denom != bridge.Stake.Denom {
		return nil, errorsmod.Wrapf(types.ErrInvalidStakeDenom, "expected %s, got %s", bridge.Stake.Denom, msg.Amount.Denom)
	}

	// 3. Transfer amount from operator to module
	operatorAddr, _ := k.addressCodec.StringToBytes(msg.Operator)
	topUpCoins := sdk.NewCoins(msg.Amount)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, operatorAddr, types.ModuleName, topUpCoins); err != nil {
		return nil, errorsmod.Wrap(err, "failed to escrow top-up stake")
	}

	// 4. Add to operator's stake
	bridge.Stake.Amount = bridge.Stake.Amount.Add(msg.Amount.Amount)
	if err := k.BridgeOperators.Set(ctx, bridgeKey, bridge); err != nil {
		return nil, err
	}

	// 5. Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeBridgeStakeToppedUp,
			sdk.NewAttribute(types.AttributeKeyOperator, msg.Operator),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyAmount, msg.Amount.String())),
	)

	return &types.MsgTopUpBridgeStakeResponse{}, nil
}
