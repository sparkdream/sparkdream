//go:build mainnet

package types

import (
	"cosmossdk.io/math"
)

// Mainnet values for trust level configuration.
// Build with: go build -tags mainnet
func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		ProvisionalMinRep:            math.LegacyNewDec(50),
		ProvisionalMinInterims:       3,
		EstablishedMinRep:            math.LegacyNewDec(200),
		EstablishedMinInterims:       10,
		TrustedMinRep:                math.LegacyNewDec(500),
		TrustedMinSeasons:            1,
		CoreMinRep:                   math.LegacyNewDec(1000),
		CoreMinSeasons:               2,
		NewInvitationCredits:         0,
		ProvisionalInvitationCredits: 3,
		EstablishedInvitationCredits: 6,
		TrustedInvitationCredits:     10,
		CoreInvitationCredits:        20,
	}
}

// getSentinelRewardEpochBlocks returns the cadence at which the sentinel SPARK
// reward pool is drained on mainnet (~1 day at 6s blocks).
func getSentinelRewardEpochBlocks() uint64 {
	return 14400
}

// getInvitationCostMultiplier returns the per-invitation cost escalation
// applied on top of MinInvitationStake. 1.1x raised to the credits-spent
// power makes spam invitations exponentially more expensive while leaving
// the first invitation at the floor.
func getInvitationCostMultiplier() math.LegacyDec {
	return math.LegacyNewDecWithPrec(110, 2) // 1.1x
}
