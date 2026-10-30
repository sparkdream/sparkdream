package types_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/types"
)

// FED-35: genesis validation catches what InitGenesis would silently
// collapse or could never settle.
func TestGenesisValidateRecords(t *testing.T) {
	hash := sha256.Sum256([]byte("phoenix"))
	valid := func() *types.GenesisState {
		gs := types.DefaultGenesis()
		gs.Peers = []types.Peer{{Id: "phoenix.example", Type: types.PeerType_PEER_TYPE_ACTIVITYPUB}}
		gs.PeerPolicies = []types.PeerPolicy{{PeerId: "phoenix.example"}}
		gs.BridgeBindings = []types.BridgeBinding{{Address: "op", PeerId: "phoenix.example"}}
		gs.FederatedContent = []types.FederatedContent{{
			Id: 4, PeerId: "phoenix.example", ContentHash: hash[:],
			Status: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED,
		}}
		gs.NextContentId = 5
		gs.VerificationRecords = []types.VerificationRecord{{
			ContentId: 4, EscrowedChallengeFee: math.NewInt(10),
			PendingVerifierVerdict:    types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_RIGHT,
			ArbiterEscalationDeadline: 100,
		}}
		gs.ArbiterEscalationQueue = []types.ContentDeadline{{Deadline: 100, ContentId: 4}}
		gs.ArbiterSubmissions = []types.ArbiterSubmissionEntry{
			{SubmitterKey: "anon:3", Submission: types.ArbiterHashSubmission{ContentId: 4, ContentHash: hash[:]}},
		}
		gs.ArbiterHashCounts = []types.ArbiterHashCount{{ContentId: 4, ContentHash: hex.EncodeToString(hash[:]), Count: 1}}
		gs.NextArbiterAnonSubmissionId = 4
		gs.IdentityLinks = []types.IdentityLink{
			{LocalAddress: "a", PeerId: "phoenix.example", RemoteIdentity: "@x@phoenix.example", Status: types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED},
			{LocalAddress: "b", PeerId: "phoenix.example", RemoteIdentity: "@x@phoenix.example", Status: types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED},
		}
		return gs
	}
	require.NoError(t, valid().Validate())
	attested := valid()
	attested.IdentityLinks[0].AttestedBy = "op"
	attested.AttesterRevocationQueue = []types.AttesterRevocation{{Operator: "op", PeerId: "phoenix.example"}}
	require.NoError(t, attested.Validate())

	for _, tc := range []struct {
		name   string
		mutate func(*types.GenesisState)
	}{
		{"duplicate peer", func(gs *types.GenesisState) { gs.Peers = append(gs.Peers, gs.Peers[0]) }},
		{"attested_by on an unverified link", func(gs *types.GenesisState) { gs.IdentityLinks[1].AttestedBy = "op" }},
		{"attester revocation without operator", func(gs *types.GenesisState) {
			gs.AttesterRevocationQueue = []types.AttesterRevocation{{PeerId: "phoenix.example"}}
		}},
		{"duplicate attester revocation", func(gs *types.GenesisState) {
			e := types.AttesterRevocation{Operator: "op", PeerId: "phoenix.example"}
			gs.AttesterRevocationQueue = []types.AttesterRevocation{e, e}
		}},
		{"policy for unknown peer", func(gs *types.GenesisState) { gs.PeerPolicies[0].PeerId = "aurora.example" }},
		{"duplicate binding", func(gs *types.GenesisState) { gs.BridgeBindings = append(gs.BridgeBindings, gs.BridgeBindings[0]) }},
		{"content for unknown peer", func(gs *types.GenesisState) { gs.FederatedContent[0].PeerId = "aurora.example" }},
		{"duplicate content", func(gs *types.GenesisState) {
			gs.FederatedContent = append(gs.FederatedContent, gs.FederatedContent[0])
		}},
		{"next_content_id reuses an id", func(gs *types.GenesisState) { gs.NextContentId = 4 }},
		{"short content hash", func(gs *types.GenesisState) { gs.FederatedContent[0].ContentHash = []byte{1} }},
		{"unknown content status", func(gs *types.GenesisState) { gs.FederatedContent[0].Status = 99 }},
		{"dispute without a record", func(gs *types.GenesisState) {
			gs.VerificationRecords = nil
			gs.ArbiterEscalationQueue = nil
		}},
		{"stashed verdict without its queue entry", func(gs *types.GenesisState) { gs.ArbiterEscalationQueue = nil }},
		{"negative escrow", func(gs *types.GenesisState) { gs.VerificationRecords[0].EscrowedChallengeFee = math.NewInt(-1) }},
		{"escalation without its jury deadline entry", func(gs *types.GenesisState) {
			gs.EscalatedChallenges = []types.EscalatedChallenge{{ContentId: 4, JuryDeadline: 50, EscrowedEscalationFee: math.ZeroInt()}}
		}},
		{"hash count does not match submissions", func(gs *types.GenesisState) { gs.ArbiterHashCounts[0].Count = 2 }},
		{"submissions without a hash count", func(gs *types.GenesisState) { gs.ArbiterHashCounts = nil }},
		{"anonymous sequence below a used key", func(gs *types.GenesisState) { gs.NextArbiterAnonSubmissionId = 3 }},
		{"duplicate identity link", func(gs *types.GenesisState) { gs.IdentityLinks = append(gs.IdentityLinks, gs.IdentityLinks[0]) }},
		{"remote identity verified twice", func(gs *types.GenesisState) {
			gs.IdentityLinks[1].Status = types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED
		}},
		{"outbound sequence reuses an id", func(gs *types.GenesisState) {
			gs.OutboundAttestations = []types.OutboundAttestation{{Id: 2, PeerId: "phoenix.example"}}
			gs.NextOutboundAttestationId = 2
		}},
		{"attestation for unknown peer", func(gs *types.GenesisState) {
			gs.ReputationAttestations = []types.ReputationAttestation{{LocalAddress: "a", PeerId: "aurora.example"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gs := valid()
			tc.mutate(gs)
			require.Error(t, gs.Validate())
		})
	}
}
