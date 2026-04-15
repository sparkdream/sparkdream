package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) RegisterBridge(ctx context.Context, msg *types.MsgRegisterBridge) (*types.MsgRegisterBridgeResponse, error) {
	// 1. Verify authority is Operations Committee
	if !k.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or Operations Committee")
	}

	if _, err := k.addressCodec.StringToBytes(msg.Operator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid operator address")
	}

	// 2. Verify peer exists and is type ACTIVITYPUB or ATPROTO
	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}
	if peer.Type != types.PeerType_PEER_TYPE_ACTIVITYPUB && peer.Type != types.PeerType_PEER_TYPE_ATPROTO {
		return nil, errorsmod.Wrapf(types.ErrPeerTypeMismatch, "bridge operators only for ActivityPub/AT Protocol peers, got %s", peer.Type)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// 3. Check max_bridges_per_peer not exceeded
	bridgeCount, err := k.countBridgesForPeer(ctx, msg.PeerId)
	if err != nil {
		return nil, err
	}
	if bridgeCount >= params.MaxBridgesPerPeer {
		return nil, errorsmod.Wrapf(types.ErrMaxBridgesExceeded, "peer %q already has %d bridges (max %d)", msg.PeerId, bridgeCount, params.MaxBridgesPerPeer)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 4. Check cooldown if previously revoked
	bridgeKey := collections.Join(msg.Operator, msg.PeerId)
	existingBridge, err := k.BridgeOperators.Get(ctx, bridgeKey)
	if err == nil {
		if existingBridge.Status != types.BridgeStatus_BRIDGE_STATUS_REVOKED {
			return nil, errorsmod.Wrapf(types.ErrBridgeAlreadyExists, "operator %s already registered for peer %s", msg.Operator, msg.PeerId)
		}
		// Check cooldown
		cooldownEnd := existingBridge.RevokedAt + int64(params.BridgeRevocationCooldown.Seconds())
		if blockTime < cooldownEnd {
			return nil, errorsmod.Wrapf(types.ErrCooldownNotElapsed, "cooldown until %d, current time %d", cooldownEnd, blockTime)
		}
	}

	// 5. Escrow min_bridge_stake from operator
	operatorAddr, _ := k.addressCodec.StringToBytes(msg.Operator)
	stakeCoins := sdk.NewCoins(params.MinBridgeStake)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, operatorAddr, types.ModuleName, stakeCoins); err != nil {
		return nil, errorsmod.Wrapf(types.ErrInsufficientStake, "operator cannot provide min stake: %v", err)
	}

	// 6. Create BridgeOperator record
	bridge := types.BridgeOperator{
		Address:      msg.Operator,
		PeerId:       msg.PeerId,
		Protocol:     msg.Protocol,
		Endpoint:     msg.Endpoint,
		Stake:        params.MinBridgeStake,
		RegisteredAt: blockTime,
		Status:       types.BridgeStatus_BRIDGE_STATUS_ACTIVE,
	}
	if err := k.BridgeOperators.Set(ctx, bridgeKey, bridge); err != nil {
		return nil, err
	}

	// Update BridgesByPeer index
	if err := k.BridgesByPeer.Set(ctx, collections.Join(msg.PeerId, msg.Operator)); err != nil {
		return nil, err
	}

	// 7. If peer was PENDING, transition to ACTIVE
	if peer.Status == types.PeerStatus_PEER_STATUS_PENDING {
		peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
		if err := k.Peers.Set(ctx, msg.PeerId, peer); err != nil {
			return nil, err
		}
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypePeerActivated,
				sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId)),
		)
	}

	// 8. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeBridgeRegistered,
			sdk.NewAttribute(types.AttributeKeyOperator, msg.Operator),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyProtocol, msg.Protocol)),
	)

	return &types.MsgRegisterBridgeResponse{}, nil
}
