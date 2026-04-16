package keeper

import (
	"context"
	"errors"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	if err := k.Port.Set(ctx, genState.PortId); err != nil {
		return err
	}
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// Peers
	for _, peer := range genState.Peers {
		if err := k.Peers.Set(ctx, peer.Id, peer); err != nil {
			return err
		}
	}

	// PeerPolicies
	for _, policy := range genState.PeerPolicies {
		if err := k.PeerPolicies.Set(ctx, policy.PeerId, policy); err != nil {
			return err
		}
	}

	// BridgeOperators
	for _, bridge := range genState.BridgeOperators {
		key := collections.Join(bridge.Address, bridge.PeerId)
		if err := k.BridgeOperators.Set(ctx, key, bridge); err != nil {
			return err
		}
		// Rebuild BridgesByPeer index
		if err := k.BridgesByPeer.Set(ctx, collections.Join(bridge.PeerId, bridge.Address)); err != nil {
			return err
		}
	}

	// FederatedContent
	for _, content := range genState.FederatedContent {
		if err := k.Content.Set(ctx, content.Id, content); err != nil {
			return err
		}
		// Rebuild content indexes
		if err := k.ContentByPeer.Set(ctx, collections.Join(content.PeerId, content.Id)); err != nil {
			return err
		}
		if content.ContentType != "" {
			if err := k.ContentByType.Set(ctx, collections.Join(content.ContentType, content.Id)); err != nil {
				return err
			}
		}
		if content.CreatorIdentity != "" {
			if err := k.ContentByCreator.Set(ctx, collections.Join(content.CreatorIdentity, content.Id)); err != nil {
				return err
			}
		}
		if content.ExpiresAt > 0 {
			if err := k.ContentExpiration.Set(ctx, collections.Join(content.ExpiresAt, content.Id)); err != nil {
				return err
			}
		}
	}

	// IdentityLinks
	for _, link := range genState.IdentityLinks {
		key := collections.Join(link.LocalAddress, link.PeerId)
		if err := k.IdentityLinks.Set(ctx, key, link); err != nil {
			return err
		}
		// Rebuild reverse index
		if err := k.IdentityLinksByRemote.Set(ctx, collections.Join(link.PeerId, link.RemoteIdentity), link.LocalAddress); err != nil {
			return err
		}
	}

	// ReputationAttestations
	for _, att := range genState.ReputationAttestations {
		key := collections.Join(att.LocalAddress, att.PeerId)
		if err := k.RepAttestations.Set(ctx, key, att); err != nil {
			return err
		}
		if att.ExpiresAt > 0 {
			if err := k.AttestationExp.Set(ctx, collections.Join3(att.ExpiresAt, att.LocalAddress, att.PeerId)); err != nil {
				return err
			}
		}
	}

	// OutboundAttestations
	for _, att := range genState.OutboundAttestations {
		if err := k.OutboundAttestations.Set(ctx, att.Id, att); err != nil {
			return err
		}
	}

	// Verifiers
	for _, v := range genState.Verifiers {
		if err := k.Verifiers.Set(ctx, v.Address, v); err != nil {
			return err
		}
	}

	// VerificationRecords
	for _, vr := range genState.VerificationRecords {
		if err := k.VerificationRecords.Set(ctx, vr.ContentId, vr); err != nil {
			return err
		}
	}

	// Sequences — use Set() directly instead of calling Next() N times (O(1) vs O(n))
	if genState.NextContentId > 0 {
		if err := k.ContentSeq.Set(ctx, genState.NextContentId); err != nil {
			return err
		}
	}
	if genState.NextOutboundAttestationId > 0 {
		if err := k.OutboundAttestSeq.Set(ctx, genState.NextOutboundAttestationId); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	genesis := types.DefaultGenesis()

	var err error
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	genesis.PortId, err = k.Port.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}

	// Export peers
	err = k.Peers.Walk(ctx, nil, func(key string, value types.Peer) (bool, error) {
		genesis.Peers = append(genesis.Peers, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export peer policies
	err = k.PeerPolicies.Walk(ctx, nil, func(key string, value types.PeerPolicy) (bool, error) {
		genesis.PeerPolicies = append(genesis.PeerPolicies, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export bridge operators
	err = k.BridgeOperators.Walk(ctx, nil, func(key collections.Pair[string, string], value types.BridgeOperator) (bool, error) {
		genesis.BridgeOperators = append(genesis.BridgeOperators, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export content
	err = k.Content.Walk(ctx, nil, func(key uint64, value types.FederatedContent) (bool, error) {
		genesis.FederatedContent = append(genesis.FederatedContent, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export identity links
	err = k.IdentityLinks.Walk(ctx, nil, func(key collections.Pair[string, string], value types.IdentityLink) (bool, error) {
		genesis.IdentityLinks = append(genesis.IdentityLinks, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export reputation attestations
	err = k.RepAttestations.Walk(ctx, nil, func(key collections.Pair[string, string], value types.ReputationAttestation) (bool, error) {
		genesis.ReputationAttestations = append(genesis.ReputationAttestations, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export outbound attestations
	err = k.OutboundAttestations.Walk(ctx, nil, func(key uint64, value types.OutboundAttestation) (bool, error) {
		genesis.OutboundAttestations = append(genesis.OutboundAttestations, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export verifiers
	err = k.Verifiers.Walk(ctx, nil, func(key string, value types.FederationVerifier) (bool, error) {
		genesis.Verifiers = append(genesis.Verifiers, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export verification records
	err = k.VerificationRecords.Walk(ctx, nil, func(key uint64, value types.VerificationRecord) (bool, error) {
		genesis.VerificationRecords = append(genesis.VerificationRecords, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export sequences
	genesis.NextContentId, err = k.ContentSeq.Peek(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	genesis.NextOutboundAttestationId, err = k.OutboundAttestSeq.Peek(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}

	return genesis, nil
}
