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
		VerifierDemotionCooldown:     10 * time.Second,       // 10s (prod: 7 days)
		VerifierUnbondCooldown:       10 * time.Second,       // 10s (prod: 14 days) — mirrors BridgeUnbondingPeriod
		VerifierOverturnBaseCooldown: 5 * time.Second,        // 5s (prod: 24h)
		ChallengeCooldown:            5 * time.Second,        // 5s (prod: 7 days)

		// Arbiter — short windows
		ArbiterResolutionWindow: 15 * time.Second,       // 15s (prod: 24h)
		ArbiterEscalationWindow: 20 * time.Second,       // 20s (prod: 48h)
		EscalationFeeAmount:     math.NewInt(1_000_000), // 1 SPARK in bond-denom micro-units (prod: 100)

		// Rate limiting — short window
		RateLimitWindow: 30 * time.Second, // 30s (prod: 24h)

		// IBC
		IBCPacketTimeout: 10 * time.Second, // 10s (prod: 10 min)

		// Verifier-bond economics — aggressive testparams so RECOVERY
		// auto-bond + cap-scaling tests can run quickly. A verifier
		// bonded at min_bond can be slashed once and lands in RECOVERY
		// (between recovery_threshold and min_bond). The reward cap
		// (7 DREAM) is set so 2 eligible verifiers (each entitled to
		// 5 DREAM) trigger pro-rata scaling.
		MinVerifierBond:              math.NewInt(100_000_000), // 100 DREAM (prod: 500)
		VerifierRecoveryThreshold:    math.NewInt(50_000_000),  // 50 DREAM  (prod: 250)
		VerifierSlashAmount:          math.NewInt(20_000_000),  // 20 DREAM  (prod: 50)
		MinEpochVerifications:        uint32(1),                // 1 verif   (prod: 3)
		MinVerifierAccuracy:          math.LegacyZeroDec(),     // 0%        (prod: 0.8) — lets overturned verifiers earn under fresh activity in RECOVERY test
		VerifierDreamReward:          math.NewInt(5_000_000),   // 5 DREAM   (prod: 5)
		MaxVerifierDreamMintPerEpoch: math.NewInt(7_000_000),   // 7 DREAM   (prod: 100) — forces scaling with 2+ eligibles
	}
}

// getVerifierRewardEpochBlocks returns the cadence at which Phase 10 fires.
// Testparams uses a short epoch (10 blocks ≈ 60s) so the RECOVERY auto-bond
// test can wait through two reward epochs (slash in epoch N, auto-bond in
// epoch N+1) inside ~2 minutes of wall time. Production cadence is ~7 days.
func getVerifierRewardEpochBlocks() uint64 {
	return 10
}
