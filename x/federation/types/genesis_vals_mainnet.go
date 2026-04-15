//go:build mainnet

package types

import (
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Mainnet values — production parameters from spec Section 13.
// Build with: go build -tags mainnet
func getFederationGenesisParams() federationGenesisParams {
	return federationGenesisParams{
		MinBridgeStake:           sdk.NewCoin("uspark", math.NewInt(1_000_000_000)), // 1000 SPARK
		BridgeRevocationCooldown: 7 * 24 * time.Hour,                                // 7 days
		BridgeUnbondingPeriod:    14 * 24 * time.Hour,                               // 14 days

		ContentTTL:     90 * 24 * time.Hour, // 90 days
		AttestationTTL: 30 * 24 * time.Hour, // 30 days

		MaxIdentityLinksPerUser: uint32(10),
		UnverifiedLinkTTL:      30 * 24 * time.Hour, // 30 days
		ChallengeTTL:           7 * 24 * time.Hour,  // 7 days

		VerificationWindow:          24 * time.Hour,
		ChallengeWindow:             7 * 24 * time.Hour,  // 7 days
		ChallengeFee:                sdk.NewCoin("uspark", math.NewInt(250_000_000)), // 250 SPARK
		ChallengeJuryDeadline:       14 * 24 * time.Hour, // 14 days
		VerifierDemotionCooldown:    7 * 24 * time.Hour,  // 7 days
		VerifierOverturnBaseCooldown: 24 * time.Hour,
		ChallengeCooldown:           7 * 24 * time.Hour,  // 7 days

		ArbiterResolutionWindow: 24 * time.Hour,
		ArbiterEscalationWindow: 48 * time.Hour,
		EscalationFee:           sdk.NewCoin("uspark", math.NewInt(100_000_000)), // 100 SPARK

		RateLimitWindow: 24 * time.Hour,
		IBCPacketTimeout: 10 * time.Minute,
	}
}
