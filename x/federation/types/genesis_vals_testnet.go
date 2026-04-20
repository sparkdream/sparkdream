//go:build testnet

package types

import (
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Testnet values — approaching production but with shorter timers.
// Build with: go build -tags testnet
func getFederationGenesisParams() federationGenesisParams {
	return federationGenesisParams{
		MinBridgeStake:           sdk.NewCoin("uspark", math.NewInt(500_000_000)), // 500 SPARK
		BridgeRevocationCooldown: 3 * 24 * time.Hour,                              // 3 days
		BridgeUnbondingPeriod:    7 * 24 * time.Hour,                              // 7 days

		ContentTTL:     45 * 24 * time.Hour, // 45 days
		AttestationTTL: 15 * 24 * time.Hour, // 15 days

		MaxIdentityLinksPerUser: uint32(10),
		UnverifiedLinkTTL:      15 * 24 * time.Hour,    // 15 days
		ChallengeTTL:           3 * 24 * time.Hour,     // 3 days

		VerificationWindow:          12 * time.Hour,
		ChallengeWindow:             3 * 24 * time.Hour,    // 3 days
		ChallengeFee:                sdk.NewCoin("uspark", math.NewInt(150_000_000)), // 150 SPARK
		ChallengeJuryDeadline:       7 * 24 * time.Hour,    // 7 days
		VerifierDemotionCooldown:    3 * 24 * time.Hour,    // 3 days
		VerifierOverturnBaseCooldown: 12 * time.Hour,
		ChallengeCooldown:           3 * 24 * time.Hour,    // 3 days

		ArbiterResolutionWindow: 12 * time.Hour,
		ArbiterEscalationWindow: 24 * time.Hour,
		EscalationFee:           sdk.NewCoin("uspark", math.NewInt(50_000_000)), // 50 SPARK

		RateLimitWindow: 12 * time.Hour,
		IBCPacketTimeout: 5 * time.Minute,
	}
}
