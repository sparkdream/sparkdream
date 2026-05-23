//go:build !mainnet && !testnet && !devnet

package types

import (
	"time"

	"cosmossdk.io/math"
)

// Testing values — short TTLs and low thresholds for integration tests.
// This is the default when no build tag is specified.
//
// Bridge-operator economic values now live on x/service ServiceTypeConfig.
// Testparams seeds:
//   - min_bond:                10_000_000 uspark (10 SPARK)
//   - unbonding_period_blocks: ~15 seconds
func getFederationGenesisParams() federationGenesisParams {
	return federationGenesisParams{
		// Content — short but long enough for E2E test suites (~10 min)
		ContentTTL:     10 * time.Minute, // 10m (prod: 90 days)
		AttestationTTL: 10 * time.Minute, // 10m (prod: 30 days)

		// Identity — short but survive E2E test suites
		MaxIdentityLinksPerUser: uint32(3),        // 3 (prod: 10)
		UnverifiedLinkTTL:       10 * time.Minute, // 10m (prod: 30 days)
		ChallengeTTL:            5 * time.Minute,  // 5m (prod: 7 days)

		// Verification — short but survive E2E test suites
		VerificationWindow:           5 * time.Minute,        // 5m (prod: 24h)
		ChallengeWindow:              5 * time.Minute,        // 5m (prod: 7 days)
		ChallengeFeeAmount:           math.NewInt(1_000_000), // 1 SPARK in bond-denom micro-units (prod: 250)
		ChallengeJuryDeadline:        15 * time.Second,       // 15s (prod: 14 days)
		VerifierDemotionCooldown:     10 * time.Second,                              // 10s (prod: 7 days)
		VerifierUnbondCooldown:       10 * time.Second,                              // 10s (prod: 14 days) — mirrors BridgeUnbondingPeriod
		VerifierOverturnBaseCooldown: 5 * time.Second,                               // 5s (prod: 24h)
		ChallengeCooldown:            5 * time.Second,                               // 5s (prod: 7 days)

		// Arbiter — short windows
		ArbiterResolutionWindow: 15 * time.Second,       // 15s (prod: 24h)
		ArbiterEscalationWindow: 20 * time.Second,       // 20s (prod: 48h)
		EscalationFeeAmount:     math.NewInt(1_000_000), // 1 SPARK in bond-denom micro-units (prod: 100)

		// Rate limiting — short window
		RateLimitWindow: 30 * time.Second, // 30s (prod: 24h)

		// IBC
		IBCPacketTimeout: 10 * time.Second, // 10s (prod: 10 min)
	}
}
