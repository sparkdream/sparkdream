package federation

import (
	"time"

	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"sparkdream/x/federation/types"
)

// simFederationParams adapts the default params to the simulation's clock.
//
// A simulated block advances the block time by 1.4 to 2.8 hours, so 200
// blocks span two to three weeks, while the untagged build's windows are
// seconds to minutes long: every verification, challenge, arbitration and
// identity-link window would close one block after it opened. These windows
// are set to the production (mainnet) values, which last tens of blocks.
func simFederationParams() types.Params {
	const day = 24 * time.Hour
	p := types.DefaultParams()
	p.ContentTtl = 90 * day
	p.AttestationTtl = 30 * day
	p.UnverifiedLinkTtl = 30 * day
	p.ChallengeTtl = 7 * day
	p.VerificationWindow = 2 * day
	p.ChallengeWindow = 3 * day
	p.ChallengeJuryDeadline = 7 * day
	p.ArbiterResolutionWindow = day
	p.ArbiterEscalationWindow = 2 * day
	p.RateLimitWindow = day
	return p
}

// simPeers returns the simulated genesis peers and their policies: one
// ACTIVE peer per bridged protocol the simulation exercises (ActivityPub, AT
// Protocol) and one ACTIVE Spark Dream (IBC) peer with no channel (the
// simulation has no IBC, so its packet paths are never exercised). Only a
// group policy address or x/gov activates a peer, which no simulated
// operation signs, so without these every bridge, content, verification and
// identity-link operation would be a NoOp. The policies admit every
// federable content type in both directions and every author, with generous
// rate limits; the Spark Dream peer also takes reputation queries and
// attestations.
func simPeers(simState *module.SimulationState) ([]types.Peer, []types.PeerPolicy) {
	gov := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	now := simState.GenTimestamp.Unix()

	var federable []string
	for _, ct := range types.DefaultKnownContentTypes {
		if ct != "reveal_proposal" && ct != "reveal_tranche" {
			federable = append(federable, ct)
		}
	}

	specs := []struct {
		id, name string
		typ      types.PeerType
	}{
		{"phoenix.social", "Phoenix Social", types.PeerType_PEER_TYPE_ACTIVITYPUB},
		{"zenith.social", "Zenith Social", types.PeerType_PEER_TYPE_ATPROTO},
		{"aurora-1", "Aurora", types.PeerType_PEER_TYPE_SPARK_DREAM},
	}
	peers := make([]types.Peer, 0, len(specs))
	policies := make([]types.PeerPolicy, 0, len(specs))
	for _, s := range specs {
		peers = append(peers, types.Peer{
			Id:           s.id,
			DisplayName:  s.name,
			Type:         s.typ,
			Status:       types.PeerStatus_PEER_STATUS_ACTIVE,
			RegisteredAt: now,
			LastActivity: now,
			RegisteredBy: gov,
		})
		sparkDream := s.typ == types.PeerType_PEER_TYPE_SPARK_DREAM
		policies = append(policies, types.PeerPolicy{
			PeerId:                       s.id,
			OutboundContentTypes:         append([]string(nil), federable...),
			InboundContentTypes:          append([]string(nil), federable...),
			InboundRateLimitPerEpoch:     1000,
			OutboundRateLimitPerEpoch:    1000,
			AllowReputationQueries:       sparkDream,
			AcceptReputationAttestations: sparkDream,
			AllowedIdentities:            []string{types.AllIdentities},
		})
	}
	return peers, policies
}
