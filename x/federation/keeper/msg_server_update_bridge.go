package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UpdateBridge(ctx context.Context, msg *types.MsgUpdateBridge) (*types.MsgUpdateBridgeResponse, error) {
	// 1. Verify authority is Operations Committee
	if !k.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or Operations Committee")
	}

	// 2. Verify bridge exists and is ACTIVE
	bridgeKey := collections.Join(msg.Operator, msg.PeerId)
	bridge, err := k.BridgeOperators.Get(ctx, bridgeKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not found for peer %s", msg.Operator, msg.PeerId)
	}
	if bridge.Status != types.BridgeStatus_BRIDGE_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotActive, "bridge status is %s", bridge.Status)
	}

	// 3. Update non-empty fields
	if msg.Endpoint != "" {
		bridge.Endpoint = msg.Endpoint
	}

	if err := k.BridgeOperators.Set(ctx, bridgeKey, bridge); err != nil {
		return nil, err
	}

	// 4. Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeBridgeUpdated,
			sdk.NewAttribute(types.AttributeKeyOperator, msg.Operator),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId)),
	)

	return &types.MsgUpdateBridgeResponse{}, nil
}
