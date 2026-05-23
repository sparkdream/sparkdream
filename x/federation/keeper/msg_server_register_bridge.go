package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
)

// RegisterBridge orchestrates bridge registration. Operator-signed.
//
// Phase 3 of the federation→service migration plan. Step ordering is
// load-bearing: any call into x/service that can fire a hook back
// into federation must happen AFTER the new BridgeBinding (and its
// reverse-index entry) is written, otherwise the hook handler iterates
// BindingsByOperator and silently misses the in-flight binding.
//
// Flow:
//  1. Load peer; validate type; check max_bridges_per_peer; resolve
//     controller := peer.controller_group ?? OpsComm policy address.
//  2. Map peer.type → service_type (federation-bridge-activitypub /
//     federation-bridge-atproto).
//  3. Look up existing service.Operator for (operator, service_type):
//     - Exists: validate the existing controller matches the resolved
//     controller. Defer any TopUpBond to step 5.
//     - New: call serviceKeeper.RegisterOperator (escrows bond, no
//     hooks fire). Skip step 5.
//  4. Write BridgeBinding + BindingsByOperator reverse index. After
//     this point any hook firing on the operator sees the new binding.
//  5. Exists branch only, when msg.stake > 0: call serviceKeeper.TopUpBond.
//     If the operator was UNDERFUNDED this fires AfterOperatorReFunded
//     synchronously; the hook iterates BindingsByOperator and now
//     correctly includes the binding written in step 4.
//  6. Transition peer PENDING→ACTIVE on first binding.
func (k msgServer) RegisterBridge(ctx context.Context, msg *types.MsgRegisterBridge) (*types.MsgRegisterBridgeResponse, error) {
	operatorBytes, err := k.addressCodec.StringToBytes(msg.Operator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid operator address")
	}

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

	// max_bridges_per_peer kill-switch check (Decision 6).
	bridgeCount, err := k.countBridgesForPeer(ctx, msg.PeerId)
	if err != nil {
		return nil, err
	}
	if bridgeCount >= params.MaxBridgesPerPeer {
		return nil, errorsmod.Wrapf(types.ErrMaxBridgesExceeded, "peer %q already has %d bridges (max %d)", msg.PeerId, bridgeCount, params.MaxBridgesPerPeer)
	}

	bridgeKey := collections.Join(msg.Operator, msg.PeerId)
	if _, err := k.BridgeBindings.Get(ctx, bridgeKey); err == nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeAlreadyExists, "binding %s for peer %s already exists", msg.Operator, msg.PeerId)
	}

	// Resolve service_type from peer type. The mapping is fixed: each
	// supported peer type has exactly one service_type seeded at
	// genesis (Decision 1).
	serviceType, err := serviceTypeForPeer(peer.Type)
	if err != nil {
		return nil, err
	}

	// Resolve controller: peer.controller_group if non-empty, else the
	// Operations Committee policy address. The resolved value is
	// captured on the service.Operator at registration time and stays
	// stable across the operator's lifetime — see MsgUpdatePeerController
	// for the "new bridges only" semantics.
	controller, err := k.resolveControllerForPeer(ctx, peer)
	if err != nil {
		return nil, err
	}

	sk := k.serviceKeeper()
	if sk == nil {
		// Standalone mode (unit tests without service wired). Skip the
		// service-side work entirely and just write the binding —
		// callers must understand this is registration-only without
		// economic bond.
		return k.writeBridgeBindingOnly(ctx, msg, serviceType)
	}

	// Step 3: check existing service.Operator.
	existing, exists := sk.GetOperator(ctx, operatorBytes, serviceType)
	if exists {
		// Controller mismatch is a hard error (Decision 1a). The
		// operator cannot bridge two peers under different controllers
		// using the same address.
		if existing.Controller != controller {
			return nil, errorsmod.Wrapf(types.ErrControllerMismatch,
				"operator already has a %s service.Operator with controller=%s; "+
					"peer %s expects controller=%s. Use a different address for "+
					"the new peer, or transfer the existing Operator's controller "+
					"via service.MsgOpenControllerTransferCase.",
				serviceType, existing.Controller, msg.PeerId, controller)
		}
	} else {
		// New service.Operator. RegisterOperator escrows the bond,
		// validates min_bond against the ServiceTypeConfig, and
		// creates the Operator record. Does NOT fire hooks, so it's
		// safe to call before we write the binding in step 4.
		metadata := []byte("federation:" + msg.PeerId) // small, audit-friendly
		if _, err := sk.RegisterOperator(ctx, msg.Operator, serviceType, controller, msg.StakeAmount, metadata, types.ServiceSourceNormal); err != nil {
			return nil, err
		}
	}

	// Step 4: write BridgeBinding + reverse index BEFORE any further
	// service call that may fire hooks (re-entrance protection).
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	binding := types.BridgeBinding{
		Address:      msg.Operator,
		PeerId:       msg.PeerId,
		Protocol:     msg.Protocol,
		Endpoint:     msg.Endpoint,
		RegisteredAt: sdkCtx.BlockTime().Unix(),
	}
	if err := k.BridgeBindings.Set(ctx, bridgeKey, binding); err != nil {
		return nil, err
	}
	if err := k.BridgesByPeer.Set(ctx, collections.Join(msg.PeerId, msg.Operator)); err != nil {
		return nil, err
	}
	if err := k.BindingsByOperator.Set(ctx, collections.Join3(serviceType, msg.Operator, msg.PeerId)); err != nil {
		return nil, err
	}

	// Step 5: if reusing an existing operator and the registrant
	// supplied extra stake, top up now (after the binding is written).
	if exists && msg.StakeAmount.IsPositive() {
		if err := sk.TopUpBond(ctx, operatorBytes, serviceType, msg.StakeAmount); err != nil {
			return nil, err
		}
	}

	// Step 6: peer PENDING → ACTIVE transition on first binding.
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

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeBridgeRegistered,
			sdk.NewAttribute(types.AttributeKeyOperator, msg.Operator),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyProtocol, msg.Protocol)),
	)

	return &types.MsgRegisterBridgeResponse{}, nil
}

// serviceTypeForPeer maps a federation PeerType to the x/service
// service_type seeded at genesis. SPARK_DREAM peers don't use bridge
// operators (IBC handles federation natively), so they reject here.
func serviceTypeForPeer(peerType types.PeerType) (string, error) {
	switch peerType {
	case types.PeerType_PEER_TYPE_ACTIVITYPUB:
		return types.ServiceTypeFederationBridgeActivityPub, nil
	case types.PeerType_PEER_TYPE_ATPROTO:
		return types.ServiceTypeFederationBridgeATProto, nil
	default:
		return "", errorsmod.Wrapf(types.ErrPeerTypeMismatch, "no service_type mapping for peer type %s", peerType)
	}
}

// resolveControllerForPeer returns peer.ControllerGroup if non-empty
// and valid (revalidated at consumption time since the peer record was
// validated at registration but a Group could theoretically have been
// dissolved since); otherwise falls back to the Operations Committee
// policy address resolved via commonsKeeper.GetCouncilPolicyAddress.
//
// Note: this captures the resolved address at MsgRegisterBridge time,
// not at peer-registration time. If the OpsComm Group's policy address
// changes mid-flight (committee migration), existing bridges keep their
// originally-captured controller. Future-bridges get the new resolved
// address.
func (k Keeper) resolveControllerForPeer(ctx context.Context, peer types.Peer) (string, error) {
	ck := k.late.commonsKeeper
	if ck == nil {
		return "", errorsmod.Wrap(types.ErrNotAuthorized, "commons keeper not wired")
	}
	if peer.ControllerGroup != "" {
		// Revalidate at consumption time.
		if !ck.IsGroupPolicyAddress(ctx, peer.ControllerGroup) {
			return "", errorsmod.Wrapf(types.ErrNotAuthorized, "peer %q controller_group %q is no longer a valid group policy address", peer.Id, peer.ControllerGroup)
		}
		return peer.ControllerGroup, nil
	}
	// Default: Operations Committee policy address.
	addr, ok := ck.GetCouncilPolicyAddress(ctx, "commons", "operations")
	if !ok {
		return "", errorsmod.Wrap(types.ErrNotAuthorized, "Operations Committee policy address not resolvable (group not seeded?)")
	}
	return addr, nil
}

// writeBridgeBindingOnly is the standalone-mode fallback path used in
// unit tests where the service keeper isn't wired. Writes the federation-
// side binding without escrowing a bond.
func (k msgServer) writeBridgeBindingOnly(ctx context.Context, msg *types.MsgRegisterBridge, serviceType string) (*types.MsgRegisterBridgeResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	bridgeKey := collections.Join(msg.Operator, msg.PeerId)
	binding := types.BridgeBinding{
		Address:      msg.Operator,
		PeerId:       msg.PeerId,
		Protocol:     msg.Protocol,
		Endpoint:     msg.Endpoint,
		RegisteredAt: sdkCtx.BlockTime().Unix(),
	}
	if err := k.BridgeBindings.Set(ctx, bridgeKey, binding); err != nil {
		return nil, err
	}
	if err := k.BridgesByPeer.Set(ctx, collections.Join(msg.PeerId, msg.Operator)); err != nil {
		return nil, err
	}
	if err := k.BindingsByOperator.Set(ctx, collections.Join3(serviceType, msg.Operator, msg.PeerId)); err != nil {
		return nil, err
	}

	peer, _ := k.Peers.Get(ctx, msg.PeerId)
	if peer.Status == types.PeerStatus_PEER_STATUS_PENDING {
		peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
		_ = k.Peers.Set(ctx, msg.PeerId, peer)
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypePeerActivated,
				sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId)),
		)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeBridgeRegistered,
			sdk.NewAttribute(types.AttributeKeyOperator, msg.Operator),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyProtocol, msg.Protocol)),
	)
	return &types.MsgRegisterBridgeResponse{}, nil
}
