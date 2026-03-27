//go:build testnet

package types

import (
	"cosmossdk.io/math"
)

// Testnet values — 2x devnet thresholds, approaching production difficulty.
// Build with: go build -tags testnet
func getTrustLevelConfig() TrustLevelConfig {
	return TrustLevelConfig{
		ProvisionalMinRep:            math.LegacyNewDec(50),   // production: 50
		ProvisionalMinInterims:       3,                       // production: 3
		EstablishedMinRep:            math.LegacyNewDec(200),  // production: 200
		EstablishedMinInterims:       10,                      // production: 10
		TrustedMinRep:                math.LegacyNewDec(500),  // production: 500
		TrustedMinSeasons:            2,                       // production: 1
		CoreMinRep:                   math.LegacyNewDec(1000), // production: 1000
		CoreMinSeasons:               2,                       // production: 2
		NewInvitationCredits:         0,
		ProvisionalInvitationCredits: 4,  // production: 1
		EstablishedInvitationCredits: 8,  // production: 3
		TrustedInvitationCredits:     14, // production: 5
		CoreInvitationCredits:        30, // production: 10
	}
}
