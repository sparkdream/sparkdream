package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) SlashBridge(ctx context.Context, msg *types.MsgSlashBridge) (*types.MsgSlashBridgeResponse, error) {
	// 1. Verify authority is Operations Committee
	if !k.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or Operations Committee")
	}

	bridgeKey := collections.Join(msg.Operator, msg.PeerId)
	bridge, err := k.BridgeOperators.Get(ctx, bridgeKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not found for peer %s", msg.Operator, msg.PeerId)
	}

	// Slashing works on both ACTIVE and UNBONDING bridges
	if bridge.Status != types.BridgeStatus_BRIDGE_STATUS_ACTIVE &&
		bridge.Status != types.BridgeStatus_BRIDGE_STATUS_UNBONDING {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotActive, "bridge status is %s, cannot slash", bridge.Status)
	}

	// 2. Verify slash amount does not exceed remaining stake
	slashAmount := msg.Amount
	currentStake := bridge.Stake.Amount
	if slashAmount.GT(currentStake) {
		return nil, errorsmod.Wrapf(types.ErrSlashExceedsStake, "slash %s exceeds stake %s", slashAmount, currentStake)
	}

	// 3. Burn the slashed amount
	slashCoins := sdk.NewCoins(sdk.NewCoin(bridge.Stake.Denom, slashAmount))
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, slashCoins); err != nil {
		return nil, errorsmod.Wrap(err, "failed to burn slashed coins")
	}

	// Update stake
	bridge.Stake.Amount = currentStake.Sub(slashAmount)
	bridge.SlashCount++

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 4. Emit slash event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeBridgeSlashed,
			sdk.NewAttribute(types.AttributeKeyOperator, msg.Operator),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyAmount, slashAmount.String()),
			sdk.NewAttribute(types.AttributeKeyReason, msg.Reason)),
	)

	// 5. Auto-revocation check: if remaining stake < min_bridge_stake
	if bridge.Stake.Amount.LT(params.MinBridgeStake.Amount) && bridge.Status == types.BridgeStatus_BRIDGE_STATUS_ACTIVE {
		bridge.Status = types.BridgeStatus_BRIDGE_STATUS_UNBONDING
		bridge.RevokedAt = blockTime
		bridge.UnbondingEndTime = blockTime + int64(params.BridgeUnbondingPeriod.Seconds())

		if err := k.BridgeUnbondingQueue.Set(ctx, collections.Join3(bridge.UnbondingEndTime, msg.Operator, msg.PeerId)); err != nil {
			return nil, err
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeBridgeAutoRevoked,
				sdk.NewAttribute(types.AttributeKeyOperator, msg.Operator),
				sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
				sdk.NewAttribute(types.AttributeKeyReason, fmt.Sprintf("stake %s below minimum %s after slash", bridge.Stake.Amount, params.MinBridgeStake.Amount))),
		)
	}

	if err := k.BridgeOperators.Set(ctx, bridgeKey, bridge); err != nil {
		return nil, err
	}

	return &types.MsgSlashBridgeResponse{}, nil
}
