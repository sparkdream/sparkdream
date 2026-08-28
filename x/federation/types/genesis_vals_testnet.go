//go:build testnet

package types

import (
	"time"

	"cosmossdk.io/math"
)

// Testnet values — approaching production but with shorter timers.
// Build with: go build -tags testnet
//
// Bridge-operator economic values now live on x/service ServiceTypeConfig.
// Testnet seeds:
//   - min_bond:                500_000_000 uspark (500 SPARK)
//   - unbonding_period_blocks: ~7 days
func getFederationGenesisParams() federationGenesisParams {
	return federationGenesisParams{
		ContentTTL:     45 * 24 * time.Hour, // 45 days
		AttestationTTL: 15 * 24 * time.Hour, // 15 days

		MaxIdentityLinksPerUser: uint32(10),
		UnverifiedLinkTTL:       15 * 24 * time.Hour, // 15 days
		ChallengeTTL:            3 * 24 * time.Hour,  // 3 days

		VerificationWindow:           12 * time.Hour,
		ChallengeWindow:              3 * 24 * time.Hour,       // 3 days
		ChallengeFeeAmount:           math.NewInt(150_000_000), // 150 SPARK in bond-denom micro-units
		ChallengeJuryDeadline:        7 * 24 * time.Hour,       // 7 days
		VerifierDemotionCooldown:     3 * 24 * time.Hour,       // 3 days
		VerifierUnbondCooldown:       7 * 24 * time.Hour,       // 7 days — mirrors BridgeUnbondingPeriod
		VerifierOverturnBaseCooldown: 12 * time.Hour,
		ChallengeCooldown:            3 * 24 * time.Hour, // 3 days

		ArbiterResolutionWindow: 12 * time.Hour,
		ArbiterEscalationWindow: 24 * time.Hour,
		EscalationFeeAmount:     math.NewInt(50_000_000), // 50 SPARK in bond-denom micro-units

		RateLimitWindow:  12 * time.Hour,
		IBCPacketTimeout: 5 * time.Minute,

		// Verifier-bond economics — spec defaults
		MinVerifierBond:           math.NewInt(500_000_000), // 500 DREAM
		VerifierRecoveryThreshold: math.NewInt(250_000_000), // 250 DREAM
		VerifierSlashAmount:       math.NewInt(50_000_000),  // 50 DREAM
		OperatorRewardEpochBlocks: 7200,
	}
}
