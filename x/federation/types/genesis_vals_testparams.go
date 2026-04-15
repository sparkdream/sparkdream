//go:build !mainnet && !testnet && !devnet

package types

import (
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Testing values — short TTLs and low thresholds for integration tests.
// This is the default when no build tag is specified.
func getFederationGenesisParams() federationGenesisParams {
	return federationGenesisParams{
		// Bridge — low stake and short cooldowns for fast test cycles
		MinBridgeStake:           sdk.NewCoin("uspark", math.NewInt(10_000_000)), // 10 SPARK (prod: 1000)
		BridgeRevocationCooldown: 10 * time.Second,                               // 10s (prod: 7 days)
		BridgeUnbondingPeriod:    15 * time.Second,                               // 15s (prod: 14 days)

		// Content — short but long enough for E2E test suites (~10 min)
		ContentTTL:     10 * time.Minute, // 10m (prod: 90 days)
		AttestationTTL: 10 * time.Minute, // 10m (prod: 30 days)

		// Identity — short but survive E2E test suites
		MaxIdentityLinksPerUser: uint32(3),           // 3 (prod: 10)
		UnverifiedLinkTTL:      10 * time.Minute,     // 10m (prod: 30 days)
		ChallengeTTL:           5 * time.Minute,      // 5m (prod: 7 days)

		// Verification — short but survive E2E test suites
		VerificationWindow:          5 * time.Minute,  // 5m (prod: 24h)
		ChallengeWindow:             5 * time.Minute,  // 5m (prod: 7 days)
		ChallengeFee:                sdk.NewCoin("uspark", math.NewInt(1_000_000)), // 1 SPARK (prod: 250)
		ChallengeJuryDeadline:       15 * time.Second, // 15s (prod: 14 days)
		VerifierDemotionCooldown:    10 * time.Second, // 10s (prod: 7 days)
		VerifierOverturnBaseCooldown: 5 * time.Second, // 5s (prod: 24h)
		ChallengeCooldown:           5 * time.Second,  // 5s (prod: 7 days)

		// Arbiter — short windows
		ArbiterResolutionWindow: 15 * time.Second, // 15s (prod: 24h)
		ArbiterEscalationWindow: 20 * time.Second, // 20s (prod: 48h)
		EscalationFee:           sdk.NewCoin("uspark", math.NewInt(1_000_000)), // 1 SPARK (prod: 100)

		// Rate limiting — short window
		RateLimitWindow: 30 * time.Second, // 30s (prod: 24h)

		// IBC
		IBCPacketTimeout: 10 * time.Second, // 10s (prod: 10 min)
	}
}
