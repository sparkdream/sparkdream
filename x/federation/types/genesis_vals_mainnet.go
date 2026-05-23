//go:build mainnet

package types

import (
	"time"

	"cosmossdk.io/math"
)

// Mainnet values — production parameters from spec Section 13.
// Build with: go build -tags mainnet
//
// MinBridgeStake / BridgeRevocationCooldown / BridgeUnbondingPeriod
// were dropped in the federation→service migration. The corresponding
// ServiceTypeConfig values seeded at x/service genesis are:
//   - min_bond:                 1_000_000_000 uspark (1000 SPARK)
//   - unbonding_period_blocks:  ~14 days
//   - (revocation cooldown is no longer a separate concept; service
//     enforces re-registration via HasSlashedRecord on the prior pair)
func getFederationGenesisParams() federationGenesisParams {
	return federationGenesisParams{
		ContentTTL:     90 * 24 * time.Hour, // 90 days
		AttestationTTL: 30 * 24 * time.Hour, // 30 days

		MaxIdentityLinksPerUser: uint32(10),
		UnverifiedLinkTTL:       30 * 24 * time.Hour, // 30 days
		ChallengeTTL:            7 * 24 * time.Hour,  // 7 days

		VerificationWindow:           24 * time.Hour,
		ChallengeWindow:              7 * 24 * time.Hour,         // 7 days
		ChallengeFeeAmount:           math.NewInt(250_000_000),   // 250 SPARK in bond-denom micro-units
		ChallengeJuryDeadline:        14 * 24 * time.Hour,        // 14 days
		VerifierDemotionCooldown:     7 * 24 * time.Hour,                              // 7 days
		VerifierUnbondCooldown:       14 * 24 * time.Hour,                             // 14 days — mirrors BridgeUnbondingPeriod
		VerifierOverturnBaseCooldown: 24 * time.Hour,
		ChallengeCooldown:            7 * 24 * time.Hour, // 7 days

		ArbiterResolutionWindow: 24 * time.Hour,
		ArbiterEscalationWindow: 48 * time.Hour,
		EscalationFeeAmount:     math.NewInt(100_000_000), // 100 SPARK in bond-denom micro-units

		RateLimitWindow:  24 * time.Hour,
		IBCPacketTimeout: 10 * time.Minute,
	}
}
