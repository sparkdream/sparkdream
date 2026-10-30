package types

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// validateRecords checks the imported records for what InitGenesis cannot
// repair: duplicates (Set would collapse them silently), records naming a
// peer that does not exist, sequences that would hand out an id already in
// use, out-of-range enums, malformed hashes and negative escrow, and the
// lifecycle queues a dispute in flight cannot settle without. The FED-03
// InitGenesis rebuilds cover the secondary indexes; this covers the
// semantics those rebuilds take on trust.
func (gs GenesisState) validateRecords() error {
	peers := make(map[string]struct{}, len(gs.Peers))
	for _, p := range gs.Peers {
		if p.Id == "" {
			return fmt.Errorf("peer with empty id")
		}
		if _, dup := peers[p.Id]; dup {
			return fmt.Errorf("duplicated peer %s", p.Id)
		}
		if _, ok := PeerType_name[int32(p.Type)]; !ok {
			return fmt.Errorf("peer %s: unknown type %d", p.Id, p.Type)
		}
		if _, ok := PeerStatus_name[int32(p.Status)]; !ok {
			return fmt.Errorf("peer %s: unknown status %d", p.Id, p.Status)
		}
		peers[p.Id] = struct{}{}
	}
	knownPeer := func(what, id string) error {
		if _, ok := peers[id]; !ok {
			return fmt.Errorf("%s references unknown peer %q", what, id)
		}
		return nil
	}

	policies := make(map[string]struct{}, len(gs.PeerPolicies))
	for _, p := range gs.PeerPolicies {
		if _, dup := policies[p.PeerId]; dup {
			return fmt.Errorf("duplicated peer policy %s", p.PeerId)
		}
		if err := knownPeer("peer policy", p.PeerId); err != nil {
			return err
		}
		policies[p.PeerId] = struct{}{}
	}

	bindings := make(map[string]struct{}, len(gs.BridgeBindings))
	for _, b := range gs.BridgeBindings {
		key := b.Address + "/" + b.PeerId
		if _, dup := bindings[key]; dup {
			return fmt.Errorf("duplicated bridge binding (%s, %s)", b.Address, b.PeerId)
		}
		if err := knownPeer("bridge binding "+b.Address, b.PeerId); err != nil {
			return err
		}
		bindings[key] = struct{}{}
	}

	contentStatus := make(map[uint64]FederatedContentStatus, len(gs.FederatedContent))
	var maxContentID uint64
	for _, c := range gs.FederatedContent {
		if _, dup := contentStatus[c.Id]; dup {
			return fmt.Errorf("duplicated federated content %d", c.Id)
		}
		if err := knownPeer(fmt.Sprintf("content %d", c.Id), c.PeerId); err != nil {
			return err
		}
		if _, ok := FederatedContentStatus_name[int32(c.Status)]; !ok {
			return fmt.Errorf("content %d: unknown status %d", c.Id, c.Status)
		}
		if len(c.ContentHash) != 0 && len(c.ContentHash) != ContentHashSize {
			return fmt.Errorf("content %d: content hash has %d bytes, want %d", c.Id, len(c.ContentHash), ContentHashSize)
		}
		contentStatus[c.Id] = c.Status
		if c.Id > maxContentID {
			maxContentID = c.Id
		}
	}
	if len(gs.FederatedContent) > 0 && gs.NextContentId <= maxContentID {
		return fmt.Errorf("next_content_id %d must exceed the highest content id %d", gs.NextContentId, maxContentID)
	}

	links := make(map[string]struct{}, len(gs.IdentityLinks))
	verifiedRemote := make(map[string]string)
	for _, l := range gs.IdentityLinks {
		key := l.LocalAddress + "/" + l.PeerId
		if _, dup := links[key]; dup {
			return fmt.Errorf("duplicated identity link (%s, %s)", l.LocalAddress, l.PeerId)
		}
		if err := knownPeer("identity link "+l.LocalAddress, l.PeerId); err != nil {
			return err
		}
		if _, ok := IdentityLinkStatus_name[int32(l.Status)]; !ok {
			return fmt.Errorf("identity link (%s, %s): unknown status %d", l.LocalAddress, l.PeerId, l.Status)
		}
		if l.AttestedBy != "" && l.Status != IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED {
			return fmt.Errorf("identity link (%s, %s) has attested_by but is %s", l.LocalAddress, l.PeerId, l.Status)
		}
		if l.Status == IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED {
			rk := l.PeerId + "/" + l.RemoteIdentity
			if other, taken := verifiedRemote[rk]; taken {
				return fmt.Errorf("remote identity %s on peer %s is verified to both %s and %s", l.RemoteIdentity, l.PeerId, other, l.LocalAddress)
			}
			verifiedRemote[rk] = l.LocalAddress
		}
		links[key] = struct{}{}
	}

	atts := make(map[string]struct{}, len(gs.ReputationAttestations))
	for _, a := range gs.ReputationAttestations {
		key := a.LocalAddress + "/" + a.PeerId
		if _, dup := atts[key]; dup {
			return fmt.Errorf("duplicated reputation attestation (%s, %s)", a.LocalAddress, a.PeerId)
		}
		if err := knownPeer("reputation attestation "+a.LocalAddress, a.PeerId); err != nil {
			return err
		}
		atts[key] = struct{}{}
	}

	outbound := make(map[uint64]struct{}, len(gs.OutboundAttestations))
	var maxOutbound uint64
	for _, o := range gs.OutboundAttestations {
		if _, dup := outbound[o.Id]; dup {
			return fmt.Errorf("duplicated outbound attestation %d", o.Id)
		}
		if err := knownPeer(fmt.Sprintf("outbound attestation %d", o.Id), o.PeerId); err != nil {
			return err
		}
		outbound[o.Id] = struct{}{}
		if o.Id > maxOutbound {
			maxOutbound = o.Id
		}
	}
	if len(gs.OutboundAttestations) > 0 && gs.NextOutboundAttestationId <= maxOutbound {
		return fmt.Errorf("next_outbound_attestation_id %d must exceed the highest outbound attestation id %d", gs.NextOutboundAttestationId, maxOutbound)
	}

	challenges := make(map[string]struct{}, len(gs.PendingIdentityChallenges))
	for _, c := range gs.PendingIdentityChallenges {
		key := c.ClaimedAddress + "/" + c.ClaimantChainPeerId
		if _, dup := challenges[key]; dup {
			return fmt.Errorf("duplicated pending identity challenge (%s, %s)", c.ClaimedAddress, c.ClaimantChainPeerId)
		}
		if err := knownPeer("pending identity challenge "+c.ClaimedAddress, c.ClaimantChainPeerId); err != nil {
			return err
		}
		challenges[key] = struct{}{}
	}

	queueSet := func(entries []ContentDeadline) map[ContentDeadline]struct{} {
		m := make(map[ContentDeadline]struct{}, len(entries))
		for _, e := range entries {
			m[e] = struct{}{}
		}
		return m
	}
	escalationQueue := queueSet(gs.ArbiterEscalationQueue)
	juryQueue := queueSet(gs.EscalatedChallengeDeadlineQueue)

	records := make(map[uint64]struct{}, len(gs.VerificationRecords))
	for _, r := range gs.VerificationRecords {
		if _, dup := records[r.ContentId]; dup {
			return fmt.Errorf("duplicated verification record %d", r.ContentId)
		}
		if !r.EscrowedChallengeFee.IsNil() && r.EscrowedChallengeFee.IsNegative() {
			return fmt.Errorf("verification record %d: negative escrowed challenge fee", r.ContentId)
		}
		if _, ok := PendingVerifierVerdict_name[int32(r.PendingVerifierVerdict)]; !ok {
			return fmt.Errorf("verification record %d: unknown pending verdict %d", r.ContentId, r.PendingVerifierVerdict)
		}
		// A stashed verdict is applied only through its escalation-queue
		// entry; without it the dispute (and its escrow) would never settle.
		if r.PendingVerifierVerdict != PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED {
			if _, ok := escalationQueue[ContentDeadline{Deadline: r.ArbiterEscalationDeadline, ContentId: r.ContentId}]; !ok {
				return fmt.Errorf("verification record %d: stashed verdict has no arbiter escalation queue entry at %d", r.ContentId, r.ArbiterEscalationDeadline)
			}
		}
		records[r.ContentId] = struct{}{}
	}
	for id, status := range contentStatus {
		if status == FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED ||
			status == FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED {
			if _, ok := records[id]; !ok {
				return fmt.Errorf("content %d is %s but has no verification record", id, status)
			}
		}
	}

	escalated := make(map[uint64]struct{}, len(gs.EscalatedChallenges))
	for _, e := range gs.EscalatedChallenges {
		if _, dup := escalated[e.ContentId]; dup {
			return fmt.Errorf("duplicated escalated challenge %d", e.ContentId)
		}
		if !e.EscrowedEscalationFee.IsNil() && e.EscrowedEscalationFee.IsNegative() {
			return fmt.Errorf("escalated challenge %d: negative escrowed escalation fee", e.ContentId)
		}
		if _, ok := juryQueue[ContentDeadline{Deadline: e.JuryDeadline, ContentId: e.ContentId}]; !ok {
			return fmt.Errorf("escalated challenge %d has no jury deadline queue entry at %d", e.ContentId, e.JuryDeadline)
		}
		escalated[e.ContentId] = struct{}{}
	}

	// Arbiter submissions: unique keys, anonymous keys below the sequence,
	// and tallies that match the submissions they count.
	subs := make(map[string]struct{}, len(gs.ArbiterSubmissions))
	tally := make(map[string]uint32)
	var maxAnon uint64
	anonSeen := false
	for _, s := range gs.ArbiterSubmissions {
		key := fmt.Sprintf("%d/%s", s.Submission.ContentId, s.SubmitterKey)
		if _, dup := subs[key]; dup {
			return fmt.Errorf("duplicated arbiter submission %s", key)
		}
		subs[key] = struct{}{}
		if rest, ok := strings.CutPrefix(s.SubmitterKey, "anon:"); ok {
			n, err := strconv.ParseUint(rest, 10, 64)
			if err != nil {
				return fmt.Errorf("arbiter submission %s: malformed anonymous key", key)
			}
			anonSeen = true
			if n > maxAnon {
				maxAnon = n
			}
		}
		tally[fmt.Sprintf("%d/%s", s.Submission.ContentId, hex.EncodeToString(s.Submission.ContentHash))]++
	}
	if anonSeen && gs.NextArbiterAnonSubmissionId <= maxAnon {
		return fmt.Errorf("next_arbiter_anon_submission_id %d must exceed the highest anonymous submission %d", gs.NextArbiterAnonSubmissionId, maxAnon)
	}
	counted := make(map[string]struct{}, len(gs.ArbiterHashCounts))
	for _, hc := range gs.ArbiterHashCounts {
		key := fmt.Sprintf("%d/%s", hc.ContentId, hc.ContentHash)
		if _, dup := counted[key]; dup {
			return fmt.Errorf("duplicated arbiter hash count %s", key)
		}
		counted[key] = struct{}{}
		if tally[key] != hc.Count {
			return fmt.Errorf("arbiter hash count %s is %d but %d submissions carry that hash", key, hc.Count, tally[key])
		}
	}
	for key, n := range tally {
		if _, ok := counted[key]; !ok {
			return fmt.Errorf("%d arbiter submissions for %s have no hash count", n, key)
		}
	}

	removal := make(map[string]struct{}, len(gs.PeerRemovalQueue))
	for _, e := range gs.PeerRemovalQueue {
		if _, dup := removal[e.PeerId]; dup {
			return fmt.Errorf("duplicated peer removal entry %s", e.PeerId)
		}
		if err := knownPeer("peer removal entry", e.PeerId); err != nil {
			return err
		}
		removal[e.PeerId] = struct{}{}
	}

	revocations := make(map[string]struct{}, len(gs.AttesterRevocationQueue))
	for _, e := range gs.AttesterRevocationQueue {
		if e.Operator == "" || e.PeerId == "" {
			return fmt.Errorf("attester revocation entry (%q, %q) needs an operator and a peer", e.Operator, e.PeerId)
		}
		key := e.Operator + "/" + e.PeerId
		if _, dup := revocations[key]; dup {
			return fmt.Errorf("duplicated attester revocation entry (%s, %s)", e.Operator, e.PeerId)
		}
		revocations[key] = struct{}{}
	}
	return nil
}
