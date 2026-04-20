//go:build devnet

package types

import (
	"cosmossdk.io/math"
)

// Devnet values — most permissive of the three networks for fast iteration.
// Trust thresholds sit below testnet/mainnet; invitation credits sit above
// them so dev work never bumps into invite caps.
// Build with: go build -tags devnet
func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		ProvisionalMinRep:            math.LegacyNewDec(25),  // production: 50
		ProvisionalMinInterims:       2,                      // production: 3
		EstablishedMinRep:            math.LegacyNewDec(100), // production: 200
		EstablishedMinInterims:       5,                      // production: 10
		TrustedMinRep:                math.LegacyNewDec(250), // production: 500
		TrustedMinSeasons:            1,                      // production: 1
		CoreMinRep:                   math.LegacyNewDec(500), // production: 1000
		CoreMinSeasons:               1,                      // production: 2
		NewInvitationCredits:         0,
		ProvisionalInvitationCredits: 5,  // mainnet: 3, testnet: 4
		EstablishedInvitationCredits: 10, // mainnet: 6, testnet: 8
		TrustedInvitationCredits:     18, // mainnet: 10, testnet: 14
		CoreInvitationCredits:        40, // mainnet: 20, testnet: 30
	}
}

// getSentinelRewardEpochBlocks returns the cadence at which the sentinel SPARK
// reward pool is drained on devnet (~6h at 6s blocks — fastest of the three
// networks so devs can observe a full epoch in a single working session).
func getSentinelRewardEpochBlocks() uint64 {
	return 3600
}
