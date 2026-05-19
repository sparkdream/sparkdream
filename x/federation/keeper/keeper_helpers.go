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

// IsGovAuthority reports whether the bech32 address string matches the
// configured x/gov authority bytes. Used by Phase 1 federation→service
// migration messages (UpdatePeerController, ResyncBridgeCount,
// PruneOrphanBindings) that accept gov authority.
func (k Keeper) IsGovAuthority(addr string) bool {
	authorityBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	return bytes.Equal(k.authority, authorityBytes)
}

// countBridgesForPeer counts the number of bindings registered for a
// peer. Status filtering is no longer needed: bindings only exist for
// non-terminal operators (the AfterOperatorDissolved/Retired hooks
// prune them on terminal transitions).
func (k Keeper) countBridgesForPeer(ctx context.Context, peerID string) (uint64, error) {
	var count uint64
	rng := collections.NewPrefixedPairRange[string, string](peerID)
	err := k.BridgesByPeer.Walk(ctx, rng, func(_ collections.Pair[string, string]) (bool, error) {
		count++
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
