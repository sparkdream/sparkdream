package keeper

import (
	"bytes"
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/federation/types"
)

// isGovernance checks if the given address bytes match the module authority (governance).
func (k Keeper) isGovernance(authorityBytes []byte) bool {
	return bytes.Equal(k.authority, authorityBytes)
}

// isCouncilAuthorized checks if the address is authorized via governance or Commons Council.
func (k Keeper) IsCouncilAuthorized(ctx context.Context, addr string, council, committee string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	if k.isGovernance(addrBytes) {
		return true
	}
	if k.late.commonsKeeper == nil {
		return false
	}
	return k.late.commonsKeeper.IsCouncilAuthorized(ctx, addr, council, committee)
}

// countBridgesForPeer counts the number of active/unbonding bridge operators for a peer.
func (k Keeper) countBridgesForPeer(ctx context.Context, peerID string) (uint64, error) {
	var count uint64
	rng := collections.NewPrefixedPairRange[string, string](peerID)
	err := k.BridgesByPeer.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		// Check if bridge is active or unbonding (not revoked)
		bridge, err := k.BridgeOperators.Get(ctx, collections.Join(key.K2(), key.K1()))
		if err != nil {
			return false, nil // skip if not found
		}
		if bridge.Status == types.BridgeStatus_BRIDGE_STATUS_ACTIVE ||
			bridge.Status == types.BridgeStatus_BRIDGE_STATUS_SUSPENDED ||
			bridge.Status == types.BridgeStatus_BRIDGE_STATUS_UNBONDING {
			count++
		}
		return false, nil
	})
	return count, err
}

// getPeerRequireActive gets a peer and verifies it is ACTIVE.
func (k Keeper) GetPeerRequireActive(ctx context.Context, peerID string) (types.Peer, error) {
	peer, err := k.Peers.Get(ctx, peerID)
	if err != nil {
		return types.Peer{}, types.ErrPeerNotFound
	}
	if peer.Status != types.PeerStatus_PEER_STATUS_ACTIVE {
		return types.Peer{}, types.ErrPeerNotActive
	}
	return peer, nil
}
