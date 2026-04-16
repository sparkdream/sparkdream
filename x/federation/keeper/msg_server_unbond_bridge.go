package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UnbondBridge(ctx context.Context, msg *types.MsgUnbondBridge) (*types.MsgUnbondBridgeResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Operator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid operator address")
	}

	// Authorization: The proto signer annotation is `signer = "operator"`, so
	// the Cosmos SDK message router already enforces that the transaction signer
	// matches msg.Operator. No additional authorization check is needed here.

	// 1. Verify bridge exists and is ACTIVE
	bridgeKey := collections.Join(msg.Operator, msg.PeerId)
	bridge, err := k.BridgeOperators.Get(ctx, bridgeKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not found for peer %s", msg.Operator, msg.PeerId)
	}
	if bridge.Status != types.BridgeStatus_BRIDGE_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotActive, "bridge status is %s, cannot self-unbond", bridge.Status)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 2. Set bridge to UNBONDING
	bridge.Status = types.BridgeStatus_BRIDGE_STATUS_UNBONDING
	bridge.RevokedAt = blockTime
	bridge.UnbondingEndTime = blockTime + int64(params.BridgeUnbondingPeriod.Seconds())
	if err := k.BridgeOperators.Set(ctx, bridgeKey, bridge); err != nil {
		return nil, err
	}

	// 3. Add to unbonding queue
	if err := k.BridgeUnbondingQueue.Set(ctx, collections.Join3(bridge.UnbondingEndTime, msg.Operator, msg.PeerId)); err != nil {
		return nil, err
	}

	// 4. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeBridgeSelfUnbonded,
			sdk.NewAttribute(types.AttributeKeyOperator, msg.Operator),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId)),
	)

	return &types.MsgUnbondBridgeResponse{}, nil
}
