package keeper

import (
	"context"
	"errors"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
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
	for _, bridge := range genState.BridgeBindings {
		key := collections.Join(bridge.Address, bridge.PeerId)
		if err := k.BridgeBindings.Set(ctx, key, bridge); err != nil {
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

	// Verifier activity (Phase 4 bonded-role generalization: generic bond
	// state lives in x/rep, only per-module counters are tracked here).
	for _, activity := range genState.VerifierActivities {
		if err := k.VerifierActivity.Set(ctx, activity.Address, activity); err != nil {
			return err
		}
	}

	// VerificationRecords
	for _, vr := range genState.VerificationRecords {
		if err := k.VerificationRecords.Set(ctx, vr.ContentId, vr); err != nil {
			return err
		}
	}

	// Per-UTC-day community-pool draw ledger for the operator reward pool.
	// Without it an import hands the chain a fresh daily allowance, so the
	// day's draw could be taken twice across a mid-day export/import.
	for _, df := range genState.OperatorRewardDayFundingList {
		amount := df.AmountFunded
		if amount.IsNil() {
			amount = math.ZeroInt()
		}
		if err := k.OperatorRewardDayFunding.Set(ctx, df.Day, amount.String()); err != nil {
			return err
		}
	}

	// Write-through the verifier bond-role config to x/rep so MsgBondRole
	// enforcement uses federation's seeded values (Phase 4 bonded-role
	// generalization).
	if err := k.SyncVerifierBondedRoleConfig(ctx, genState.Params); err != nil {
		return err
	}

	// In-flight verification-lifecycle queues. Restored verbatim rather
	// than recomputed: the arbiter deadlines exist nowhere else, so a
	// dropped queue leaves the record permanently un-expirable and its
	// verifier's committed bond permanently reserved.
	if err := importDeadlineQueue(ctx, k.VerificationWindow, genState.VerificationWindowQueue); err != nil {
		return err
	}
	if err := importDeadlineQueue(ctx, k.ChallengeWindow, genState.ChallengeWindowQueue); err != nil {
		return err
	}
	if err := importDeadlineQueue(ctx, k.ArbiterResolutionQueue, genState.ArbiterResolutionQueue); err != nil {
		return err
	}
	if err := importDeadlineQueue(ctx, k.ArbiterEscalationQueue, genState.ArbiterEscalationQueue); err != nil {
		return err
	}
	if err := importDeadlineQueue(ctx, k.EscalatedChallengeDeadline, genState.EscalatedChallengeDeadlineQueue); err != nil {
		return err
	}
	for _, esc := range genState.EscalatedChallenges {
		if err := k.EscalatedChallenges.Set(ctx, esc.ContentId, esc); err != nil {
			return err
		}
	}
	for _, entry := range genState.ArbiterSubmissions {
		if err := k.ArbiterSubmissions.Set(ctx,
			collections.Join(entry.Submission.ContentId, entry.SubmitterKey), entry.Submission); err != nil {
			return err
		}
	}
	for _, hc := range genState.ArbiterHashCounts {
		if err := k.ArbiterHashCounts.Set(ctx,
			collections.Join(hc.ContentId, hc.ContentHash), hc.Count); err != nil {
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
	if genState.NextArbiterAnonSubmissionId > 0 {
		if err := k.ArbiterAnonSubSeq.Set(ctx, genState.NextArbiterAnonSubmissionId); err != nil {
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
	err = k.BridgeBindings.Walk(ctx, nil, func(key collections.Pair[string, string], value types.BridgeBinding) (bool, error) {
		genesis.BridgeBindings = append(genesis.BridgeBindings, value)
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

	// Export verifier activity (per-module counters). Generic bond state
	// lives in x/rep BondedRole, exported by x/rep's genesis.
	err = k.VerifierActivity.Walk(ctx, nil, func(_ string, value types.VerifierActivity) (bool, error) {
		genesis.VerifierActivities = append(genesis.VerifierActivities, value)
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

	// Export the operator reward day-funding ledger
	err = k.OperatorRewardDayFunding.Walk(ctx, nil, func(day uint64, raw string) (bool, error) {
		amount, ok := math.NewIntFromString(raw)
		if !ok {
			amount = math.ZeroInt()
		}
		genesis.OperatorRewardDayFundingList = append(genesis.OperatorRewardDayFundingList,
			types.OperatorRewardDayFunding{Day: day, AmountFunded: amount})
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export the in-flight verification-lifecycle queues. Without these an
	// export/import upgrade strands every dispute: the arbiter deadlines
	// live nowhere but the keysets, so a re-imported DISPUTED record would
	// have no queue entry to expire it and the verifier's committed bond
	// would never be released.
	if genesis.VerificationWindowQueue, err = exportDeadlineQueue(ctx, k.VerificationWindow); err != nil {
		return nil, err
	}
	if genesis.ChallengeWindowQueue, err = exportDeadlineQueue(ctx, k.ChallengeWindow); err != nil {
		return nil, err
	}
	if genesis.ArbiterResolutionQueue, err = exportDeadlineQueue(ctx, k.ArbiterResolutionQueue); err != nil {
		return nil, err
	}
	if genesis.ArbiterEscalationQueue, err = exportDeadlineQueue(ctx, k.ArbiterEscalationQueue); err != nil {
		return nil, err
	}
	if genesis.EscalatedChallengeDeadlineQueue, err = exportDeadlineQueue(ctx, k.EscalatedChallengeDeadline); err != nil {
		return nil, err
	}

	err = k.EscalatedChallenges.Walk(ctx, nil, func(_ uint64, value types.EscalatedChallenge) (bool, error) {
		genesis.EscalatedChallenges = append(genesis.EscalatedChallenges, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Partial arbiter quorum progress.
	err = k.ArbiterSubmissions.Walk(ctx, nil, func(key collections.Pair[uint64, string], value types.ArbiterHashSubmission) (bool, error) {
		genesis.ArbiterSubmissions = append(genesis.ArbiterSubmissions,
			types.ArbiterSubmissionEntry{SubmitterKey: key.K2(), Submission: value})
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	err = k.ArbiterHashCounts.Walk(ctx, nil, func(key collections.Pair[uint64, string], count uint32) (bool, error) {
		genesis.ArbiterHashCounts = append(genesis.ArbiterHashCounts, types.ArbiterHashCount{
			ContentId: key.K1(), ContentHash: key.K2(), Count: count,
		})
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
	genesis.NextArbiterAnonSubmissionId, err = k.ArbiterAnonSubSeq.Peek(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}

	return genesis, nil
}

// importDeadlineQueue restores one (deadline, content_id) keyset from its
// genesis representation.
func importDeadlineQueue(
	ctx context.Context,
	ks collections.KeySet[collections.Pair[int64, uint64]],
	entries []types.ContentDeadline,
) error {
	for _, e := range entries {
		if err := ks.Set(ctx, collections.Join(e.Deadline, e.ContentId)); err != nil {
			return err
		}
	}
	return nil
}

// exportDeadlineQueue drains one (deadline, content_id) keyset into the
// genesis representation shared by all four verification-lifecycle queues.
func exportDeadlineQueue(
	ctx context.Context,
	ks collections.KeySet[collections.Pair[int64, uint64]],
) ([]types.ContentDeadline, error) {
	var out []types.ContentDeadline
	err := ks.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		out = append(out, types.ContentDeadline{Deadline: key.K1(), ContentId: key.K2()})
		return false, nil
	})
	return out, err
}
