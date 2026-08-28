//go:build devnet

package types

import (
	"time"

	"cosmossdk.io/math"
)

// Devnet values — accelerated timers for development (5-15 minute ranges).
// Build with: go build -tags devnet
//
// Bridge-operator economic values now live on x/service ServiceTypeConfig.
// Devnet seeds:
//   - min_bond:                100_000_000 uspark (100 SPARK)
//   - unbonding_period_blocks: ~30 minutes
func getFederationGenesisParams() federationGenesisParams {
	return federationGenesisParams{
		ContentTTL:     24 * time.Hour,
		AttestationTTL: 12 * time.Hour,

		MaxIdentityLinksPerUser: uint32(10),
		UnverifiedLinkTTL:       1 * time.Hour,
		ChallengeTTL:            30 * time.Minute,

		VerificationWindow:           1 * time.Hour,
		ChallengeWindow:              2 * time.Hour,
		ChallengeFeeAmount:           math.NewInt(50_000_000), // 50 SPARK in bond-denom micro-units
		ChallengeJuryDeadline:        2 * time.Hour,
		VerifierDemotionCooldown:     30 * time.Minute,
		VerifierUnbondCooldown:       30 * time.Minute, // mirrors BridgeUnbondingPeriod (devnet)
		VerifierOverturnBaseCooldown: 15 * time.Minute,
		ChallengeCooldown:            15 * time.Minute,

		ArbiterResolutionWindow: 1 * time.Hour,
		ArbiterEscalationWindow: 2 * time.Hour,
		EscalationFeeAmount:     math.NewInt(10_000_000), // 10 SPARK in bond-denom micro-units

		RateLimitWindow:  1 * time.Hour,
		IBCPacketTimeout: 5 * time.Minute,

		// Verifier-bond economics — spec defaults
		MinVerifierBond:           math.NewInt(500_000_000), // 500 DREAM
		VerifierRecoveryThreshold: math.NewInt(250_000_000), // 250 DREAM
		VerifierSlashAmount:       math.NewInt(50_000_000),  // 50 DREAM
		OperatorRewardEpochBlocks: 3600,
	}
}
