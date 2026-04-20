//go:build devnet

package types

import (
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Devnet values — accelerated timers for development (5-15 minute ranges).
// Build with: go build -tags devnet
func getFederationGenesisParams() federationGenesisParams {
	return federationGenesisParams{
		MinBridgeStake:           sdk.NewCoin("uspark", math.NewInt(100_000_000)), // 100 SPARK
		BridgeRevocationCooldown: 15 * time.Minute,
		BridgeUnbondingPeriod:    30 * time.Minute,

		ContentTTL:     24 * time.Hour,
		AttestationTTL: 12 * time.Hour,

		MaxIdentityLinksPerUser: uint32(10),
		UnverifiedLinkTTL:      1 * time.Hour,
		ChallengeTTL:           30 * time.Minute,

		VerificationWindow:          1 * time.Hour,
		ChallengeWindow:             2 * time.Hour,
		ChallengeFee:                sdk.NewCoin("uspark", math.NewInt(50_000_000)), // 50 SPARK
		ChallengeJuryDeadline:       2 * time.Hour,
		VerifierDemotionCooldown:    30 * time.Minute,
		VerifierOverturnBaseCooldown: 15 * time.Minute,
		ChallengeCooldown:           15 * time.Minute,

		ArbiterResolutionWindow: 1 * time.Hour,
		ArbiterEscalationWindow: 2 * time.Hour,
		EscalationFee:           sdk.NewCoin("uspark", math.NewInt(10_000_000)), // 10 SPARK

		RateLimitWindow: 1 * time.Hour,
		IBCPacketTimeout: 5 * time.Minute,
	}
}
